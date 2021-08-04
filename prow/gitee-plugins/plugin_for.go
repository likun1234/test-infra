package plugins

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
)

type PluginFor struct {
	// Repos is either in the form of org/repos or just org.
	Repos []string `json:"repos" required:"true"`

	// ExcludedRepos is in the form of org/repo.
	ExcludedRepos []string `json:"excluded_repos,omitempty"`
}

// The return value will be one of the following cases:
// true,  false: the config can be applied to the org/repo
// true,  true:  the config can be applied to the org and org/repo
// false, true:  the config can be applied to the org except org/repo
// false, false: the config can be applied to neither org or org/repo
func (p PluginFor) CanApply(org, orgRepo string) (applyOrgRepo bool, applyOrg bool) {
	v := sets.NewString(p.Repos...)
	if v.Has(orgRepo) {
		applyOrgRepo = true
		return
	}

	if !v.Has(org) {
		return
	}

	applyOrg = true

	if len(p.ExcludedRepos) > 0 && sets.NewString(p.ExcludedRepos...).Has(orgRepo) {
		return
	}

	applyOrgRepo = true
	return
}

func (p PluginFor) Validate() error {
	if sets.NewString(p.Repos...).HasAny(p.ExcludedRepos...) {
		return fmt.Errorf("some org or org/repo exists in both repos and excluded_repos")
	}

	return nil
}

type IPluginFor interface {
	CanApply(org, orgRepo string) (applyOrgRepo bool, applyOrg bool)
}

func FindConfig(org, repo string, cfg []IPluginFor) int {
	fullName := fmt.Sprintf("%s/%s", org, repo)

	index := -1
	for i, item := range cfg {
		if applyOrgRepo, applyOrg := item.CanApply(org, fullName); applyOrgRepo {
			if !applyOrg {
				return i
			}
			index = i
		}
	}
	return index
}
