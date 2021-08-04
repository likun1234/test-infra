package trigger

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
	ciparser "k8s.io/test-infra/prow/ci-parser"
)

type JobStatusDescAndLabel struct {
	// Desc is the description of job result.
	Desc []string `json:"desc" required:"true"`

	// Label is the one which is responding to the status
	Label string `json:"label" required:"true"`
}

func (j JobStatusDescAndLabel) Validate() error {
	if len(j.Desc) == 0 {
		return fmt.Errorf("missing desc")
	}

	if sets.NewString(j.Desc...).Len() != len(j.Desc) {
		return fmt.Errorf("duplicate items")
	}

	if j.Label == "" {
		return fmt.Errorf("missing label")
	}

	return nil
}

func (j JobStatusDescAndLabel) empty() bool {
	return len(j.Desc) == 0 && j.Label == ""
}

type ciLabelConfig struct {
	// TitleOfCITable is the title of ci comment for pr. The format of comment
	// must have 2 or more columns, and the second column must be job result.
	//
	//   | job name | result | detail |
	//   | --- | --- | --- |
	//   | test     | success | link   |
	//
	// The value of TitleOfCITable for ci comment above is
	// `| job name | result | detail |`
	TitleOfCITable string `json:"title_of_ci_table"`

	// JobErrorStatus is the status desc when a single job is failed to be created
	JobErrorStatus JobStatusDescAndLabel `json:"job_error_status"`

	// JobRunningStatus is the status desc when a single job is running
	JobRunningStatus JobStatusDescAndLabel `json:"job_running_status"`

	// JobSuccessStatus is the status desc when a single job is successful
	JobSuccessStatus JobStatusDescAndLabel `json:"job_success_status"`

	// JobFailureStatus is the status desc when a single job is failed
	JobFailureStatus JobStatusDescAndLabel `json:"job_failure_status"`
}

type statusItem struct {
	// key
	k string

	// prioriy
	p int

	s JobStatusDescAndLabel
}

func (c ciLabelConfig) allStatus() []statusItem {
	return []statusItem{
		{k: "job_error_status", p: 4, s: c.JobErrorStatus},
		{k: "job_failure_status", p: 3, s: c.JobFailureStatus},
		{k: "job_running_status", p: 2, s: c.JobRunningStatus},
		{k: "job_success_status", p: 1, s: c.JobSuccessStatus},
	}
}

func (c ciLabelConfig) Validate() error {
	if c.TitleOfCITable == "" {
		return fmt.Errorf("missing title_of_ci_table")
	}

	items := c.allStatus()
	labels := sets.NewString()
	desc := sets.NewString()
	dn := 0
	ln := 0
	for i := range items {
		item := items[i].s

		if item.empty() {
			continue
		}

		if err := item.Validate(); err != nil {
			return fmt.Errorf("for %s: %s", items[i].k, err.Error())
		}

		labels.Insert(item.Label)
		ln += 1

		desc.Insert(item.Desc...)
		dn += len(item.Desc)
	}

	if labels.Len() != ln {
		return fmt.Errorf("duplicate labels")
	}

	if desc.Len() != dn {
		return fmt.Errorf("duplicate desc")
	}

	return nil
}

func (c ciLabelConfig) newCIParser() ciparser.CIParserImpl {
	items := c.allStatus()

	js := make([]ciparser.JobStatusDesc, 0, len(items))
	for i := range items {
		item := items[i].s

		if item.empty() {
			continue
		}
		js = append(js, ciparser.JobStatusDesc{
			Desc:     item.Desc,
			Status:   item.Label,
			Priority: items[i].p,
		})
	}

	return ciparser.CIParserImpl{
		TitleOfCITable: c.TitleOfCITable,
		JobStatus:      js,
	}
}

func (c ciLabelConfig) allLabels() sets.String {
	items := c.allStatus()

	r := sets.NewString()
	for i := range items {
		item := items[i].s

		if !item.empty() {
			r.Insert(item.Label)
		}
	}
	return r
}

func (c ciLabelConfig) isCIComment(comment string) bool {
	return ciparser.IsCIComment(c.TitleOfCITable, comment)
}
