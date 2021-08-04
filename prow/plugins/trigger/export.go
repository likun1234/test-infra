package trigger

import (
	"fmt"

	"k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/labels"
	"k8s.io/test-infra/prow/pjutil"
	"k8s.io/test-infra/prow/plugins"
)

var (
	HelpProvider = helpProvider
)

func HandlePR(c Client, trigger plugins.Trigger, pr github.PullRequestEvent, setPresubmit func([]config.Presubmit), hasApprovedPR func(org, repo string) bool) error {
	org, repo, a := orgRepoAuthor(pr.PullRequest)
	author := string(a)
	num := pr.PullRequest.Number

	baseSHA := ""
	baseSHAGetter := func() (string, error) {
		var err error
		baseSHA, err = c.GitHubClient.GetRef(org, repo, "heads/"+pr.PullRequest.Base.Ref)
		if err != nil {
			return "", fmt.Errorf("failed to get baseSHA: %v", err)
		}
		return baseSHA, nil
	}
	headSHAGetter := func() (string, error) {
		return pr.PullRequest.Head.SHA, nil
	}

	presubmits := getPresubmits(c.Logger, c.GitClient, c.Config, org+"/"+repo, baseSHAGetter, headSHAGetter)
	if len(presubmits) == 0 {
		return nil
	}
	if setPresubmit != nil {
		setPresubmit(presubmits)
	}

	if baseSHA == "" {
		if _, err := baseSHAGetter(); err != nil {
			return err
		}
	}

	// if the pr can't be merged automatically, don't run jobs
	if pr.PullRequest.Mergable != nil && (!*(pr.PullRequest.Mergable)) {
		return nil
	}

	switch pr.Action {
	case github.PullRequestActionOpened:
		// When a PR is opened, if the author is in the org then build it.
		// Otherwise, ask for "/ok-to-test". There's no need to look for previous
		// "/ok-to-test" comments since the PR was just opened!
		trustedResponse, err := TrustedUser(c.GitHubClient, trigger.OnlyOrgMembers, trigger.TrustedOrg, author, org, repo)
		if err != nil {
			return fmt.Errorf("could not check membership: %s", err)
		}

		if trustedResponse.IsTrusted {
			c.Logger.Info("Starting all jobs for new PR.")
			return buildAll(c, &pr.PullRequest, pr.GUID, baseSHA, presubmits)
		}

		c.Logger.Infof("Welcome message to PR author %q.", author)
		if err := welcomeMsg(c.GitHubClient, trigger, pr.PullRequest); err != nil {
			return fmt.Errorf("could not welcome non-org member %q: %v", author, err)
		}
	case github.PullRequestActionReopened:
		// When a PR is reopened, check that the user is in the org or that an org
		// member had said "/ok-to-test" before building, resulting in label ok-to-test.
		l, trusted, err := TrustedPullRequest(c.GitHubClient, trigger, author, org, repo, num, nil)
		if err != nil {
			return fmt.Errorf("could not validate PR: %s", err)
		} else if trusted {
			// Eventually remove need-ok-to-test
			// Does not work for TrustedUser() == true since labels are not fetched in this case
			if github.HasLabel(labels.NeedsOkToTest, l) {
				if err := c.GitHubClient.RemoveLabel(org, repo, num, labels.NeedsOkToTest); err != nil {
					return err
				}
			}
			c.Logger.Info("Starting all jobs for updated PR.")
			return buildAll(c, &pr.PullRequest, pr.GUID, baseSHA, presubmits)
		}
	case github.PullRequestActionEdited:
		// the base of the PR changed and we need to re-test it
		if hasApprovedPR(org, repo) {
			if github.HasLabel(labels.Approved, pr.PullRequest.Labels) {
				return buildAll(c, &pr.PullRequest, pr.GUID, baseSHA, presubmits)
			}
		} else {
			return buildAllIfTrusted(c, trigger, pr, baseSHA, presubmits)
		}
	case github.PullRequestActionSynchronize:
		return buildAllIfTrusted(c, trigger, pr, baseSHA, presubmits)
	case github.PullRequestActionLabeled:
		// When a PR is LGTMd, if it is untrusted then build it once.
		if pr.Label.Name == labels.LGTM {
			_, trusted, err := TrustedPullRequest(c.GitHubClient, trigger, author, org, repo, num, nil)
			if err != nil {
				return fmt.Errorf("could not validate PR: %s", err)
			} else if !trusted {
				c.Logger.Info("Starting all jobs for untrusted PR with LGTM.")
				return buildAll(c, &pr.PullRequest, pr.GUID, baseSHA, presubmits)
			}
		}
		if pr.Label.Name == labels.OkToTest {
			// When the bot adds the label from an /ok-to-test command,
			// we will trigger tests based on the comment event and do not
			// need to trigger them here from the label, as well
			botName, err := c.GitHubClient.BotName()
			if err != nil {
				return err
			}
			if author == botName {
				c.Logger.Debug("Label added by the bot, skipping.")
				return nil
			}
			return buildAll(c, &pr.PullRequest, pr.GUID, baseSHA, presubmits)
		}
	case github.PullRequestActionClosed:
		if err := abortAllJobs(c, &pr.PullRequest); err != nil {
			c.Logger.WithError(err).Error("Failed to abort jobs for closed pull request")
			return err
		}
	}
	return nil
}

func HandlePE(c Client, pe github.PushEvent, setPostsubmit func([]config.Postsubmit)) error {
	if pe.Deleted || pe.After == "0000000000000000000000000000000000000000" {
		// we should not trigger jobs for a branch deletion
		return nil
	}

	org := pe.Repo.Owner.Login
	repo := pe.Repo.Name

	shaGetter := func() (string, error) {
		return pe.After, nil
	}

	postsubmits := getPostsubmits(c.Logger, c.GitClient, c.Config, org+"/"+repo, shaGetter)
	if len(postsubmits) == 0 {
		return nil
	}

	if setPostsubmit != nil {
		setPostsubmit(postsubmits)
	}

	for _, j := range postsubmits {
		if shouldRun, err := j.ShouldRun(pe.Branch(), listPushEventChanges(pe)); err != nil {
			return err
		} else if !shouldRun {
			continue
		}
		refs := createRefs(pe)
		labels := make(map[string]string)
		for k, v := range j.Labels {
			labels[k] = v
		}
		labels[github.EventGUID] = pe.GUID
		pj := pjutil.NewProwJob(pjutil.PostsubmitSpec(j, refs), labels, j.Annotations)
		c.Logger.WithFields(pjutil.ProwJobFields(&pj)).Info("Creating a new prowjob.")
		if _, err := c.ProwJobClient.Create(&pj); err != nil {
			return err
		}
	}
	return nil
}

func HandleGenericComment(c Client, trigger plugins.Trigger, gc github.GenericCommentEvent, setPresubmit func([]config.Presubmit)) error {
	org := gc.Repo.Owner.Login
	repo := gc.Repo.Name
	number := gc.Number
	commentAuthor := gc.User.Login
	// Only take action when a comment is first created,
	// when it belongs to a PR,
	// and the PR is open.
	if gc.Action != github.GenericCommentActionCreated || !gc.IsPR || gc.IssueState != "open" {
		return nil
	}

	// Skip bot comments.
	botName, err := c.GitHubClient.BotName()
	if err != nil {
		return err
	}
	if commentAuthor == botName {
		c.Logger.Debug("Comment is made by the bot, skipping.")
		return nil
	}

	refGetter := config.NewRefGetterForGitHubPullRequest(c.GitHubClient, org, repo, number)
	presubmits := getPresubmits(c.Logger, c.GitClient, c.Config, org+"/"+repo, refGetter.BaseSHA, refGetter.HeadSHA)
	if len(presubmits) == 0 {
		return nil
	}
	if setPresubmit != nil {
		setPresubmit(presubmits)
	}

	// Skip comments not germane to this plugin
	if !pjutil.RetestRe.MatchString(gc.Body) &&
		!pjutil.OkToTestRe.MatchString(gc.Body) &&
		!pjutil.TestAllRe.MatchString(gc.Body) &&
		!mayNeedHelpComment(gc.Body) {
		matched := false
		for _, presubmit := range presubmits {
			matched = matched || presubmit.TriggerMatches(gc.Body)
			if matched {
				break
			}
		}
		if !matched {
			c.Logger.Debug("Comment doesn't match any triggering regex, skipping.")
			return nil
		}
	}

	// Skip untrusted users comments.
	trustedResponse, err := TrustedUser(c.GitHubClient, trigger.OnlyOrgMembers, trigger.TrustedOrg, commentAuthor, org, repo)
	if err != nil {
		return fmt.Errorf("error checking trust of %s: %v", commentAuthor, err)
	}

	trusted := trustedResponse.IsTrusted
	var l []github.Label
	if !trusted {
		// Skip untrusted PRs.
		l, trusted, err = TrustedPullRequest(c.GitHubClient, trigger, gc.IssueAuthor.Login, org, repo, number, nil)
		if err != nil {
			return err
		}
		if !trusted {
			resp := fmt.Sprintf("Cannot trigger testing until a trusted user reviews the PR and leaves an `/ok-to-test` message.")
			c.Logger.Infof("Commenting \"%s\".", resp)
			return c.GitHubClient.CreateComment(org, repo, number, plugins.FormatResponseRaw(gc.Body, gc.HTMLURL, gc.User.Login, resp))
		}
	}

	// At this point we can trust the PR, so we eventually update labels.
	// Ensure we have labels before test, because TrustedPullRequest() won't be called
	// when commentAuthor is trusted.
	if l == nil {
		l, err = c.GitHubClient.GetIssueLabels(org, repo, number)
		if err != nil {
			return err
		}
	}
	isOkToTest := HonorOkToTest(trigger) && pjutil.OkToTestRe.MatchString(gc.Body)
	if isOkToTest && !github.HasLabel(labels.OkToTest, l) {
		if err := c.GitHubClient.AddLabel(org, repo, number, labels.OkToTest); err != nil {
			return err
		}
	}
	if (isOkToTest || github.HasLabel(labels.OkToTest, l)) && github.HasLabel(labels.NeedsOkToTest, l) {
		if err := c.GitHubClient.RemoveLabel(org, repo, number, labels.NeedsOkToTest); err != nil {
			return err
		}
	}

	pr, err := refGetter.PullRequest()
	if err != nil {
		return err
	}
	baseSHA, err := refGetter.BaseSHA()
	if err != nil {
		return err
	}

	toTest, err := FilterPresubmits(HonorOkToTest(trigger), c.GitHubClient, gc.Body, pr, presubmits, c.Logger)
	if err != nil {
		return err
	}
	if needsHelp, note := shouldRespondWithHelp(gc.Body, len(toTest)); needsHelp {
		return addHelpComment(c.GitHubClient, gc.Body, org, repo, pr.Base.Ref, pr.Number, presubmits, gc.HTMLURL, commentAuthor, note, c.Logger)
	}
	return RunRequested(c, pr, baseSHA, toTest, gc.GUID)
}

func GetJobNum(c Client, org, repo string, prNumber int) int {
	refGetter := config.NewRefGetterForGitHubPullRequest(c.GitHubClient, org, repo, prNumber)

	presubmits := getPresubmits(
		c.Logger, c.GitClient, c.Config, org+"/"+repo,
		refGetter.BaseSHA, refGetter.HeadSHA,
	)
	return len(presubmits)
}
