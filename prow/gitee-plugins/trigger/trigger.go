package trigger

import (
	"fmt"
	"time"

	sdk "gitee.com/openeuler/go-gitee/gitee"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	prowapi "k8s.io/test-infra/prow/apis/prowjobs/v1"
	prowConfig "k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/git/v2"
	"k8s.io/test-infra/prow/gitee"
	plugins "k8s.io/test-infra/prow/gitee-plugins"
	reporter "k8s.io/test-infra/prow/job-reporter"
	"k8s.io/test-infra/prow/pluginhelp"
	originp "k8s.io/test-infra/prow/plugins"
	origint "k8s.io/test-infra/prow/plugins/trigger"
)

type prowJobClient interface {
	Create(*prowapi.ProwJob) (*prowapi.ProwJob, error)
	List(opts metav1.ListOptions) (*prowapi.ProwJobList, error)
	Update(*prowapi.ProwJob) (*prowapi.ProwJob, error)
}

type trigger struct {
	gec             giteeClient
	ghc             *ghclient
	pjc             prowJobClient
	botName         string
	gitClient       git.ClientFactory
	getProwConf     prowConfig.Getter
	getPluginConfig plugins.GetPluginConfig
}

func NewTrigger(f plugins.GetPluginConfig, f1 prowConfig.Getter, gec giteeClient, pjc prowJobClient, gitc git.ClientFactory, botName string) plugins.Plugin {
	return &trigger{
		gec:             gec,
		ghc:             &ghclient{giteeClient: gec},
		pjc:             pjc,
		botName:         botName,
		gitClient:       gitc,
		getProwConf:     f1,
		getPluginConfig: f,
	}
}

func (t *trigger) HelpProvider(enabledRepos []prowConfig.OrgRepo) (*pluginhelp.PluginHelp, error) {
	c, err := t.pluginConfig()
	if err != nil {
		return nil, err
	}

	cfg := originp.Configuration{
		Triggers: c.originalTriggersConfig(),
	}

	p, err := origint.HelpProvider(&cfg, enabledRepos)
	if err != nil {
		return nil, err
	}

	p.AddCommand(pluginhelp.Command{
		Usage:       "/check-ci",
		Description: "Forces rechecking the CI status and adding CI label if possible.",
		Featured:    true,
		WhoCanUse:   "Anyone",
		Examples:    []string{"/check-ci"},
	})
	return p, nil
}

func (t *trigger) PluginName() string {
	return origint.PluginName
}

func (t *trigger) NewPluginConfig() plugins.PluginConfig {
	return &configuration{}
}

func (t *trigger) RegisterEventHandler(p plugins.Plugins) {
	name := t.PluginName()
	p.RegisterNoteEventHandler(name, t.handleNoteEvent)
	p.RegisterPullRequestHandler(name, t.handlePullRequestEvent)
	p.RegisterPushEventHandler(name, t.handlePushEvent)
}

func (t *trigger) handleNoteEvent(e *sdk.NoteEvent, log *logrus.Entry) error {
	funcStart := time.Now()
	defer func() {
		log.WithField("duration", time.Since(funcStart).String()).Debug("Completed handleNoteEvent")
	}()

	ne := gitee.NewPRNoteEvent(e)
	if !ne.IsPullRequest() {
		return nil
	}

	org, repo := ne.GetOrgRep()
	prNumber := ne.GetPRNumber()

	c, err := t.orgRepoConfig(org, repo)
	if err != nil {
		return err
	}

	cl := t.buildOriginClient(log)
	cl.GitHubClient = &ghclient{giteeClient: t.gec, prNumber: prNumber}

	if isCheckCIComment(ne) {
		return t.handleCheckCI(
			ne, c.ciLabelConfig,
			origint.GetJobNum(cl, org, repo, prNumber),
			log,
		)
	}

	if c.ciLabelConfig.isCIComment(ne.GetComment()) {
		return t.handleCIComment(
			ne, c.ciLabelConfig,
			origint.GetJobNum(cl, org, repo, prNumber),
			log,
		)
	}

	return origint.HandleGenericComment(
		cl,
		c.originalTriggerConfig(),
		plugins.NoteEventToCommentEvent(e),
		func(m []prowConfig.Presubmit) {
			SetPresubmit(org, repo, m)
		},
	)
}

func (t *trigger) handlePullRequestEvent(e *sdk.PullRequestEvent, log *logrus.Entry) error {
	funcStart := time.Now()
	defer func() {
		log.WithField("duration", time.Since(funcStart).String()).Debug("Completed handlePullRequest")
	}()

	org, repo := gitee.GetOwnerAndRepoByPREvent(e)

	c, err := t.orgRepoConfig(org, repo)
	if err != nil {
		return err
	}

	return origint.HandlePR(
		t.buildOriginClient(log),
		c.originalTriggerConfig(),
		plugins.ConvertPullRequestEvent(e),
		func(m []prowConfig.Presubmit) {
			SetPresubmit(org, repo, m)
		},
		t.ghc.hasApprovedPR,
	)
}

func (t *trigger) handlePushEvent(e *sdk.PushEvent, log *logrus.Entry) error {
	funcStart := time.Now()
	defer func() {
		log.WithField("duration", time.Since(funcStart).String()).Debug("Completed handlePushEvent")
	}()

	return origint.HandlePE(
		t.buildOriginClient(log),
		plugins.ConvertPushEvent(e),
		func(m []prowConfig.Postsubmit) {
			setPostsubmit(e.Repository.Namespace, e.Repository.Path, m)
		},
	)
}

func (t *trigger) orgRepoConfig(org, repo string) (*pluginConfig, error) {
	cfg, err := t.pluginConfig()
	if err != nil {
		return nil, err
	}

	pc := cfg.triggerFor(org, repo)
	if pc == nil {
		return nil, fmt.Errorf("no %s plugin config for this repo:%s/%s", t.PluginName(), org, repo)
	}

	return pc, nil
}

func (t *trigger) pluginConfig() (*configuration, error) {
	c := t.getPluginConfig(t.PluginName())
	if c == nil {
		return nil, fmt.Errorf("can't find the configuration")
	}

	c1, ok := c.(*configuration)
	if !ok {
		return nil, fmt.Errorf("can't convert to configuration")
	}

	return c1, nil
}

func (t *trigger) buildOriginClient(log *logrus.Entry) origint.Client {
	return origint.Client{
		GitHubClient:  t.ghc,
		Config:        t.getProwConf(),
		ProwJobClient: t.pjc,
		Logger:        log,
		GitClient:     t.gitClient,
	}
}

func SetPresubmit(org, repo string, m []prowConfig.Presubmit) {
	/* can't write as this, or the JobBase can't be changed
	for _, i := range m {
		setJob(org, repo, &i.JobBase)
	}*/

	for i := range m {
		setJob(org, repo, &m[i].JobBase)
	}
}

func setPostsubmit(org, repo string, m []prowConfig.Postsubmit) {
	for i := range m {
		setJob(org, repo, &m[i].JobBase)
	}
}

func setJob(org, repo string, job *prowConfig.JobBase) {
	job.CloneURI = fmt.Sprintf("https://gitee.com/%s/%s", org, repo)

	if job.Annotations == nil {
		job.Annotations = make(map[string]string)
	}
	job.Annotations[reporter.JobPlatformAnnotation] = "gitee"
}
