package trigger

import (
	"regexp"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/test-infra/prow/ci-parser"
	"k8s.io/test-infra/prow/gitee"
	plugins "k8s.io/test-infra/prow/gitee-plugins"
)

var (
	checkCIRe = regexp.MustCompile(`(?mi)^/check-ci\s*$`)
)

func isCheckCIComment(ne gitee.PRNoteEvent) bool {
	if !ne.IsCreatingCommentEvent() {
		return false
	}

	return checkCIRe.MatchString(ne.GetComment())
}

func (t *trigger) handleCheckCI(e gitee.PRNoteEvent, cfg ciLabelConfig, jobNumber int, log *logrus.Entry) error {
	if !isCheckCIComment(e) {
		return nil
	}

	org, repo := e.GetOrgRep()
	comments, err := t.ghc.ListPRComments(org, repo, e.GetPRNumber())
	if err != nil {
		return err
	}

	cs := plugins.FindBotComment(comments, t.botName, cfg.isCIComment)
	n := len(cs)
	if n == 0 {
		return nil
	}

	if n > 1 {
		plugins.SortBotComments(cs)
	}

	return t.checkCICommentToAddLabel(cs[n-1].Body, e, cfg, jobNumber, log)
}

func (t *trigger) handleCIComment(e gitee.PRNoteEvent, cfg ciLabelConfig, jobNumber int, log *logrus.Entry) error {
	return t.checkCICommentToAddLabel(e.GetComment(), e, cfg, jobNumber, log)
}

func (t *trigger) checkCICommentToAddLabel(comment string, e gitee.PRNoteEvent, cfg ciLabelConfig, jobNumber int, log *logrus.Entry) error {
	if !cfg.isCIComment(comment) {
		return nil
	}

	p := cfg.newCIParser()

	status, err := ciparser.ParseCIComment(p, comment)
	if err != nil {
		return err
	}

	label := p.InferFinalStatus(status)
	if label != "" && label == cfg.JobSuccessStatus.Label && len(status) != jobNumber {
		log.Infof(
			"want to add ci success label, but the status number( %d ) != job number( %d )",
			len(status), jobNumber,
		)
		label = cfg.JobRunningStatus.Label
	}

	return t.applyCILabel(e, cfg.allLabels(), label)
}

func (t *trigger) applyCILabel(e gitee.PRNoteEvent, ciLabels sets.String, toAdd string) error {
	toRemove := sets.NewString()
	for k := range gitee.GetLabelFromEvent(e.PullRequest.Labels) {
		if ciLabels.Has(k) {
			toRemove.Insert(k)
		}
	}

	org, repo := e.GetOrgRep()
	prNumber := e.GetPRNumber()
	errs := plugins.NewMultiErrors()

	if toAdd != "" {
		if toRemove.Has(toAdd) {
			toRemove.Delete(toAdd)
		} else {
			if err := t.ghc.AddPRLabel(org, repo, prNumber, toAdd); err != nil {
				errs.AddError(err)
			}
		}
	}

	for l := range toRemove {
		if err := t.ghc.RemovePRLabel(org, repo, prNumber, l); err != nil {
			errs.AddError(err)
		}
	}

	return errs.Err()
}
