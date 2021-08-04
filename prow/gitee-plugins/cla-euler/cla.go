//Package claeuler implements a check cla plugin for openEuler community
package claeuler

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"

	sdk "gitee.com/openeuler/go-gitee/gitee"
	"github.com/sirupsen/logrus"

	prowConfig "k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/gitee"
	plugins "k8s.io/test-infra/prow/gitee-plugins"
	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/pluginhelp"
)

var (
	checkCLARe = regexp.MustCompile(`(?mi)^/check-cla\s*$`)
)

type cla struct {
	getPluginConfig plugins.GetPluginConfig
	ghc             *ghclient
}

//NewCLA create a cla_euler plugin
func NewCLA(f plugins.GetPluginConfig, gec giteeClient) plugins.Plugin {
	return &cla{
		getPluginConfig: f,
		ghc:             &ghclient{giteeClient: gec},
	}
}

func (cl *cla) HelpProvider(_ []prowConfig.OrgRepo) (*pluginhelp.PluginHelp, error) {
	pluginHelp := &pluginhelp.PluginHelp{
		Description: "The cla plugin manages the application and removal of the cla labels on pull requests. It is also responsible for warning unauthorized PR authors that they need to sign the cla before their PR will be merged.",
	}
	pluginHelp.AddCommand(pluginhelp.Command{
		Usage:       "/check-cla",
		Description: "Forces rechecking of the CLA status.",
		Featured:    true,
		WhoCanUse:   "Anyone",
		Examples:    []string{"/check-cla"},
	})
	return pluginHelp, nil
}

func (cl *cla) PluginName() string {
	return "cla_euler"
}

func (cl *cla) NewPluginConfig() plugins.PluginConfig {
	return &configuration{}
}

func (cl *cla) RegisterEventHandler(p plugins.Plugins) {
	name := cl.PluginName()
	p.RegisterNoteEventHandler(name, cl.handleNoteEvent)
	p.RegisterPullRequestHandler(name, cl.handlePullRequestEvent)
}

func (cl *cla) handleNoteEvent(e *sdk.NoteEvent, log *logrus.Entry) error {
	funcStart := time.Now()
	defer func() {
		log.WithField("duration", time.Since(funcStart).String()).Debug("Completed handleNoteEvent")
	}()

	ne := gitee.NewPRNoteEvent(e)
	if !ne.IsCreatingCommentEvent() {
		log.Debug("Event is not a creation of a comment, skipping.")
		return nil
	}

	if !ne.IsPullRequest() {
		return nil
	}

	// Only consider "/check-cla" comments.
	if !checkCLARe.MatchString(ne.GetComment()) {
		return nil
	}

	return cl.handlePullRequestComment(ne, log)
}

func (cl *cla) handlePullRequestComment(e gitee.PRNoteEvent, log *logrus.Entry) error {
	org, repo := e.GetOrgRep()
	l := gitee.GetLabelFromEvent(e.PullRequest.Labels)
	return cl.handle(org, repo, e.GetPRAuthor(), e.GetPRNumber(), l, log)
}

func (cl *cla) handlePullRequestEvent(e *sdk.PullRequestEvent, log *logrus.Entry) error {
	funcStart := time.Now()
	defer func() {
		log.WithField("duration", time.Since(funcStart).String()).Debug("Completed handlePullRequest")
	}()

	if e.PullRequest.State != "open" {
		log.Debug("Pull request state is not open, skipping...")
		return nil
	}

	action := plugins.ConvertPullRequestAction(e)
	if action != github.PullRequestActionOpened && action != github.PullRequestActionSynchronize {
		return nil
	}

	org, repo := gitee.GetOwnerAndRepoByPREvent(e)
	pr := e.PullRequest
	l := gitee.GetLabelFromEvent(e.PullRequest.Labels)
	return cl.handle(org, repo, pr.User.Login, int(pr.Number), l, log)
}

func (cl *cla) handle(org, repo, prAuthor string, prNumber int, currentLabes map[string]bool, log *logrus.Entry) error {
	cfg, err := cl.orgRepoConfig(org, repo)
	if err != nil {
		return err
	}

	unsigned, err := cl.getPrCommitsAbout(org, repo, prNumber, cfg.CheckURL)
	if err != nil {
		return err
	}

	hasCLAYes := currentLabes[cfg.CLALabelYes]
	hasCLANo := currentLabes[cfg.CLALabelNo]

	deleteSignGuide(org, repo, prNumber, cl.ghc.giteeClient)

	if len(unsigned) == 0 {
		if hasCLANo {
			if err := cl.ghc.RemoveLabel(org, repo, prNumber, cfg.CLALabelNo); err != nil {
				log.WithError(err).Warningf("Could not remove %s label.", cfg.CLALabelNo)
			}
		}

		if !hasCLAYes {
			if err := cl.ghc.AddLabel(org, repo, prNumber, cfg.CLALabelYes); err != nil {
				log.WithError(err).Warningf("Could not add %s label.", cfg.CLALabelYes)
			}
			return cl.ghc.CreateComment(org, repo, prNumber, alreadySigned(prAuthor))
		}
		return nil
	}

	if hasCLAYes {
		if err := cl.ghc.RemoveLabel(org, repo, prNumber, cfg.CLALabelYes); err != nil {
			log.WithError(err).Warningf("Could not remove %s label.", cfg.CLALabelYes)
		}
	}

	if !hasCLANo {
		if err := cl.ghc.AddLabel(org, repo, prNumber, cfg.CLALabelNo); err != nil {
			log.WithError(err).Warningf("Could not add %s label.", cfg.CLALabelNo)
		}
	}

	return cl.ghc.CreateComment(
		org, repo, prNumber,
		signGuide(cfg.SignURL, "gitee", generateUnSignComment(unsigned)),
	)
}

func (cl *cla) orgRepoConfig(org, repo string) (*pluginConfig, error) {
	cfg, err := cl.pluginConfig()
	if err != nil {
		return nil, err
	}

	pc := cfg.CLAFor(org, repo)
	if pc == nil {
		return nil, fmt.Errorf("no %s plugin config for this repo:%s/%s", cl.PluginName(), org, repo)
	}

	return pc, nil
}

func (cl *cla) pluginConfig() (*configuration, error) {
	c := cl.getPluginConfig(cl.PluginName())
	if c == nil {
		return nil, fmt.Errorf("can't find the configuration")
	}

	c1, ok := c.(*configuration)
	if !ok {
		return nil, fmt.Errorf("can't convert to configuration")
	}

	return c1, nil
}

func (cl *cla) getPrCommitsAbout(org, repo string, number int, checkURL string) ([]*sdk.PullRequestCommits, error) {
	commits, err := cl.ghc.GetCommits(org, repo, number)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, fmt.Errorf("commits is empty, cla cannot be checked")
	}

	result := map[string]bool{}
	unsigned := make([]*sdk.PullRequestCommits, 0, len(commits))
	for i := range commits {
		c := &commits[i]
		if c.Commit == nil || c.Commit.Committer == nil || c.Commit.Author == nil {
			continue
		}

		email := getAuthorOfCommit(c.Commit).Email

		if email == "" {
			unsigned = append(unsigned, c)
			continue
		}

		email = plugins.NormalEmail(email)
		if v, ok := result[email]; ok {
			if !v {
				unsigned = append(unsigned, c)
			}
			continue
		}

		b, err := isSigned(email, checkURL)
		if err != nil {
			return nil, err
		}
		result[email] = b
		if !b {
			unsigned = append(unsigned, c)
		}
	}

	return unsigned, err
}

func headOfSHA(sha string) string {
	return sha[:8]
}

func getAuthorOfCommit(c *sdk.GitCommit) *sdk.GitUser {
	if c.Committer.Email == "noreply@gitee.com" || c.Committer.Name == "Gitee" {
		return c.Author
	}
	return c.Committer
}

func isSigned(email, url string) (bool, error) {
	endpoint := fmt.Sprintf("%s?email=%s", url, email)

	resp, err := http.Get(endpoint)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	rb, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return false, fmt.Errorf("response has status %q and body %q", resp.Status, string(rb))
	}

	type signingInfo struct {
		Signed bool `json:"signed"`
	}
	var v struct {
		Data signingInfo `json:"data"`
	}

	if err := json.Unmarshal(rb, &v); err != nil {
		return false, fmt.Errorf("unmarshal failed: %s", err.Error())
	}
	return v.Data.Signed, nil
}

func generateUnSignComment(commits []*sdk.PullRequestCommits) string {
	if len(commits) == 0 {
		return ""
	}

	cs := make([]string, 0, len(commits))
	for _, c := range commits {
		cs = append(cs, fmt.Sprintf(
			"%s | %s", headOfSHA(c.Sha), c.Commit.Message,
		))
	}

	return fmt.Sprintf("The following commits have not yet signed CLA.\n%s", strings.Join(cs, "\n"))
}

func deleteSignGuide(org, repo string, number int, c giteeClient) {
	v, err := c.ListPRComments(org, repo, number)
	if err != nil {
		return
	}

	prefix := signGuideTitle()

	for i := range v {
		item := &v[i]
		if strings.HasPrefix(item.Body, prefix) {
			c.DeletePRComment(org, repo, int(item.Id))
		}
	}
}

func signGuideTitle() string {
	return "Thanks for your pull request. Before we can look at your pull request, you'll need to sign a Contributor License Agreement (CLA)."
}

func signGuide(signURL, platform, cInfo string) string {
	s := `%s

%s

:memo: **Please access [here](%s) to sign the CLA.**

It may take a couple minutes for the CLA signature to be fully registered; after that, please reply here with a new comment: **/check-cla** to verify. Thanks.

---

- Please, firstly see the [**FAQ**](https://github.com/opensourceways/test-infra/blob/sync-5-22/prow/gitee-plugins/cla-euler/faq.md) to help you handle the problem.
- If you've already signed a CLA, it's possible you're using a different email address for your %s account. Check your existing CLA data and verify the email. 
- If you signed the CLA as an employee or a member of an organization, please contact your corporation or organization to verify you have been activated to start contributing.
- If you have done the above and are still having issues with the CLA being reported as unsigned, please feel free to file an issue.
	`

	return fmt.Sprintf(s, signGuideTitle(), cInfo, signURL, platform)
}

func alreadySigned(user string) string {
	s := `***@%s***, Thanks for your pull request. All the authors of commits have finished signinig CLA successfully. :wave: `
	return fmt.Sprintf(s, user)
}
