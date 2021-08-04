package ciparser

import (
	"fmt"
	"strings"
)

const (
	spliter    = "|"
	rowSpliter = "\n"
)

type CIParser interface {
	GetEachJobComment(string) ([]string, error)
	ParseJobStatus(string) (string, error)
}

func ParseCIComment(t CIParser, comment string) ([]string, error) {
	cs, err := t.GetEachJobComment(comment)
	if err != nil {
		return nil, err
	}

	r := make([]string, 0, len(cs))
	for _, c := range cs {
		if status, err := t.ParseJobStatus(c); err == nil {
			r = append(r, status)
		}
	}

	return r, nil
}

type JobStatusDesc struct {
	Desc     []string
	Status   string
	Priority int
}

func (j JobStatusDesc) isDescMatched(desc string) bool {
	for _, item := range j.Desc {
		if strings.Contains(desc, item) {
			return true
		}
	}
	return false
}

type CIParserImpl struct {
	// TitleOfCITable is the title of ci comment for pr. The format of comment
	// must have 2 or more columns, and the second column must be job result.
	//
	//   | job name | result | detail |
	//   | --- | --- | --- |
	//   | test     | success | link   |
	//
	// The value of TitleOfCITable for ci comment above is
	// `| job name | result | detail |`
	TitleOfCITable string

	JobStatus []JobStatusDesc
}

func (p CIParserImpl) GetEachJobComment(c string) ([]string, error) {
	v := strings.Split(c, p.TitleOfCITable+rowSpliter)
	if len(v) != 2 {
		return nil, fmt.Errorf("invalid CI comment")
	}

	items := strings.Split(v[1], rowSpliter)
	n := len(items)
	if n < 2 {
		return nil, fmt.Errorf("invalid table")
	}

	num := p.numOfColumns()
	for i := n - 1; i > 0; i-- {
		if _, err := parseJobResult(items[i], num); err == nil {
			// The items[0] is like | --- | --- |, so ignore it.
			return items[1 : i+1], nil
		}
	}

	return nil, fmt.Errorf("empty table")
}

func (p CIParserImpl) ParseJobStatus(c string) (string, error) {
	desc, err := parseJobResult(c, p.numOfColumns())
	if err != nil {
		return "", err
	}

	for _, v := range p.JobStatus {
		if v.isDescMatched(desc) {
			return v.Status, nil
		}
	}
	return "", fmt.Errorf("unknown job description")
}

func (p CIParserImpl) InferFinalStatus(status []string) string {
	sn := make(map[string]bool)
	for _, item := range status {
		sn[item] = true
	}

	cp := -1
	s := ""
	for _, item := range p.JobStatus {
		if sn[item.Status] && (s == "" || item.Priority > cp) {
			cp = item.Priority
			s = item.Status
		}
	}
	return s
}

func (p CIParserImpl) numOfColumns() int {
	return len(strings.Split(p.TitleOfCITable, spliter))
}

// parseJobResult return the single job result.
// The format of job comment must be "| job name | result |".
func parseJobResult(s string, n int) (string, error) {
	if m := strings.Split(s, spliter); len(m) == n && m[0] == "" {
		return m[2], nil
	}
	return "", fmt.Errorf("invalid job comment")
}

func IsCIComment(title, c string) bool {
	return strings.Count(c, title+rowSpliter) == 1
}
