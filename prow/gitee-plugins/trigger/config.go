package trigger

import (
	"fmt"

	"k8s.io/test-infra/prow/gitee-plugins"
	originp "k8s.io/test-infra/prow/plugins"
)

type configuration struct {
	Triggers []pluginConfig `json:"triggers,omitempty"`
}

func (c *configuration) Validate() error {
	for _, p := range c.Triggers {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c *configuration) SetDefault() {
}

func (c *configuration) triggerFor(org, repo string) *pluginConfig {
	v := make([]plugins.IPluginFor, 0, len(c.Triggers))
	for i := range c.Triggers {
		v = append(v, &c.Triggers[i])
	}

	if i := plugins.FindConfig(org, repo, v); i >= 0 {
		return &c.Triggers[i]
	}
	return nil
}

func (c *configuration) originalTriggersConfig() []originp.Trigger {
	triggers := make([]originp.Trigger, 0, len(c.Triggers))
	for _, item := range c.Triggers {
		triggers = append(triggers, item.originalTriggerConfig())
	}
	return triggers
}

type pluginConfig struct {
	plugins.PluginFor

	// JoinOrgURL is a link that redirects users to a location where they
	// should be able to read more about joining the organization in order
	// to become trusted members.
	JoinOrgURL string `json:"join_org_url" required:"true"`

	// OrgMemberURL is a link that show all the members of org.
	OrgMemberURL string `json:"org_member_url" required:"true"`

	// EnableLabelForCI is the tag which indicates whether enables
	// function to add ci status label for PR. If is true, the labels
	// which stand for running and fail must be set.
	EnableLabelForCI bool `json:"enable_label_for_ci"`

	ciLabelConfig
}

func (p pluginConfig) originalTriggerConfig() originp.Trigger {
	return originp.Trigger{
		// JoinOrgURL and OrgMemberURL will not change for different repos,
		// so it can assign Repos directly.
		Repos:        p.Repos,
		JoinOrgURL:   p.JoinOrgURL,
		OrgMemberURL: p.OrgMemberURL,
	}
}

func (p *pluginConfig) Validate() error {
	if err := p.PluginFor.Validate(); err != nil {
		return err
	}

	if p.JoinOrgURL == "" {
		return fmt.Errorf("missing join_org_url")
	}

	if p.OrgMemberURL == "" {
		return fmt.Errorf("missing org_member_url")
	}

	if !p.EnableLabelForCI {
		return nil
	}

	return p.ciLabelConfig.Validate()
}
