package ciparser

import (
	"errors"
	"testing"
)

const (
	testStatusError   = "error"
	testStatusRunning = "running"
	testStatusFailure = "failure"
	testStatusSuccess = "success"
)

var testImpl = CIParserImpl{
	TitleOfCITable: "| Check Name | Result | Details |",
	JobStatus: []JobStatusDesc{
		{
			Desc:     []string{"Error starting Jenkins job"},
			Status:   testStatusError,
			Priority: 4,
		},

		{
			Desc:     []string{"job failed", "job aborted"},
			Status:   testStatusFailure,
			Priority: 3,
		},

		{
			Desc:     []string{"job running", "job enqueued"},
			Status:   testStatusRunning,
			Priority: 2,
		},

		{
			Desc:     []string{"job succeeded"},
			Status:   testStatusSuccess,
			Priority: 1,
		},
	},
}

type testCaseOfParser struct {
	name              string
	comment           string
	expectError       error
	expectStatus      []string
	expectFinalStatus string
}

func doTest(t *testing.T, test testCaseOfParser) {
	s, err := ParseCIComment(testImpl, test.comment)
	if test.expectError != nil {
		if err == nil {
			t.Errorf("Run test case: %s.\nexpect an err:\n    %s\nbut got:\n    nil\n", test.name, test.expectError.Error())
			return
		}

		if test.expectError.Error() != err.Error() {
			t.Errorf("Run test case: %s.\nexpect an err:\n    %s\nbut got:\n    %s\n", test.name, test.expectError.Error(), err.Error())
			return
		}

	} else if err != nil {
		t.Errorf("Run test case: %s.\nexpect an err:\n    nil\nbut got:\n    %s\n", test.name, err.Error())
		return
	}

	if len(test.expectStatus) != len(s) {
		t.Errorf("Run test case: %s. expect %d statuses, but got:%d\n", test.name, len(test.expectStatus), len(s))
		return
	}

	for i, item := range test.expectStatus {
		if item != s[i] {
			t.Errorf("Run test case: %s.\nexpect status:    %v\nbut got:    %v\n", test.name, test.expectStatus, s)
			return
		}
	}

	fs := testImpl.InferFinalStatus(s)
	if test.expectFinalStatus != fs {
		t.Errorf("Run test case: %s. expect final status:%s, but got:%s\n", test.name, test.expectFinalStatus, fs)
	}
}

func TestNormal(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:              "normal",
		comment:           "| Check Name | Result | Details |\n| --- | --- | --- |\n| :x: hetu-core-build-test-job1 | Jenkins job failed. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job1/2635/console) |\n| :white_check_mark: hetu-core-build-test-job2 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job2/2659/console) |\n| :x: hetu-core-build-test-job3 | Jenkins job aborted. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job3/2625/console) |\n| :white_check_mark: hetu-core-build-test-job5 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job5/2777/console) |\n| :x: hetu-core-build-test-job6 | Jenkins job aborted. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job6/2628/console) |\n| :white_check_mark: hetu-core-build-test-job7 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job7/2702/console) |\n| :white_check_mark: scanoss | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/scanoss/746/console) |\n  \u003Cdetails\u003Ebase sha:0085bde869ab4adc746a7be6b0578479be0b5b4a\nhead sha: ed4650db35b2ed91e44974d2d655794c371fc9f6\u003C/details\u003E",
		expectStatus:      []string{testStatusFailure, testStatusSuccess, testStatusFailure, testStatusSuccess, testStatusFailure, testStatusSuccess, testStatusSuccess},
		expectFinalStatus: testStatusFailure,
	})
}

func TestHasPrefixBeforeTable(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:              "has prefix before table",
		comment:           "prefeix \n| Check Name | Result | Details |\n| --- | --- | --- |\n| :x: hetu-core-build-test-job1 | Jenkins job failed. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job1/2635/console) |\n| :white_check_mark: hetu-core-build-test-job2 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job2/2659/console) |\n| :x: hetu-core-build-test-job3 | Jenkins job aborted. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job3/2625/console) |\n| :white_check_mark: hetu-core-build-test-job5 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job5/2777/console) |\n| :x: hetu-core-build-test-job6 | Jenkins job aborted. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job6/2628/console) |\n| :white_check_mark: hetu-core-build-test-job7 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job7/2702/console) |\n| :white_check_mark: scanoss | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/scanoss/746/console) |\n  \u003Cdetails\u003Ebase sha:0085bde869ab4adc746a7be6b0578479be0b5b4a\nhead sha: ed4650db35b2ed91e44974d2d655794c371fc9f6\u003C/details\u003E",
		expectStatus:      []string{testStatusFailure, testStatusSuccess, testStatusFailure, testStatusSuccess, testStatusFailure, testStatusSuccess, testStatusSuccess},
		expectFinalStatus: testStatusFailure,
	})

}

func TestInvlidCIComment(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:        "invalid ci comment",
		comment:     "| Check Name | Result | Details |\n| Check Name | Result | Details |\n| --- | --- | --- |\n| :x: hetu-core-build-test-job1 | Jenkins job failed. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job1/2635/console) |\n| :white_check_mark: hetu-core-build-test-job2 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job2/2659/console) |\n| :x: hetu-core-build-test-job3 | Jenkins job aborted. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job3/2625/console) |\n| :white_check_mark: hetu-core-build-test-job5 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job5/2777/console) |\n| :x: hetu-core-build-test-job6 | Jenkins job aborted. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job6/2628/console) |\n| :white_check_mark: hetu-core-build-test-job7 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job7/2702/console) |\n| :white_check_mark: scanoss | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/scanoss/746/console) |\n  \u003Cdetails\u003Ebase sha:0085bde869ab4adc746a7be6b0578479be0b5b4a\nhead sha: ed4650db35b2ed91e44974d2d655794c371fc9f6\u003C/details\u003E",
		expectError: errors.New("invalid CI comment"),
	})
}

func TestInvalidTable(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:        "invalid table",
		comment:     "| Check Name | Result | Details |\n| --- | --- | --- |",
		expectError: errors.New("invalid table"),
	})
}

func TestEmptyTable(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:        "empty table",
		comment:     "| Check Name | Result | Details |\n| --- | --- | --- |\n",
		expectError: errors.New("empty table"),
	})
}

func TestUnknownJobDesc(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:    "unkown job description",
		comment: "| Check Name | Result | Details |\n| --- | --- | --- |\n| test | unknown | details |",
	})
}

func TestUnknownJobDesc1(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:              "unkown job description 1",
		comment:           "| Check Name | Result | Details |\n| --- | --- | --- |\n| :x: hetu-core-build-test-job1 | Jenkins job unkown. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job1/2635/console) |\n| :white_check_mark: hetu-core-build-test-job2 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job2/2659/console) |\n| :x: hetu-core-build-test-job3 | Jenkins job aborted. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job3/2625/console) |\n| :white_check_mark: hetu-core-build-test-job5 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job5/2777/console) |\n| :x: hetu-core-build-test-job6 | Jenkins job aborted. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job6/2628/console) |\n| :white_check_mark: hetu-core-build-test-job7 | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/hetu-core-build-test-job7/2702/console) |\n| :white_check_mark: scanoss | Jenkins job succeeded. | [details](https://build.openlookeng.io/job/scanoss/746/console) |\n  \u003Cdetails\u003Ebase sha:0085bde869ab4adc746a7be6b0578479be0b5b4a\nhead sha: ed4650db35b2ed91e44974d2d655794c371fc9f6\u003C/details\u003E",
		expectStatus:      []string{testStatusSuccess, testStatusFailure, testStatusSuccess, testStatusFailure, testStatusSuccess, testStatusSuccess},
		expectFinalStatus: testStatusFailure,
	})
}

func TestFSError(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:              "final status error",
		comment:           "| Check Name | Result | Details |\n| --- | --- | --- |\n| hetu-core-build-test-job1 | Error starting Jenkins job. | details |\n| hetu-core-build-test-job2 | Jenkins job succeeded. | details |",
		expectStatus:      []string{testStatusError, testStatusSuccess},
		expectFinalStatus: testStatusError,
	})
}

func TestFSFailure(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:              "final status failure",
		comment:           "| Check Name | Result | Details |\n| --- | --- | --- |\n| hetu-core-build-test-job1 | job aborted. | details |\n| hetu-core-build-test-job2 | Jenkins job succeeded. | details |",
		expectStatus:      []string{testStatusFailure, testStatusSuccess},
		expectFinalStatus: testStatusFailure,
	})
}

func TestFSRunning(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:              "final status running",
		comment:           "| Check Name | Result | Details |\n| --- | --- | --- |\n| hetu-core-build-test-job1 | job running. | details |\n| hetu-core-build-test-job2 | Jenkins job succeeded. | details |",
		expectStatus:      []string{testStatusRunning, testStatusSuccess},
		expectFinalStatus: testStatusRunning,
	})
}

func TestFSSuccess(t *testing.T) {
	doTest(t, testCaseOfParser{
		name:              "final status success",
		comment:           "| Check Name | Result | Details |\n| --- | --- | --- |\n| hetu-core-build-test-job1 | job succeeded. | details |\n| hetu-core-build-test-job2 | Jenkins job succeeded. | details |",
		expectStatus:      []string{testStatusSuccess, testStatusSuccess},
		expectFinalStatus: testStatusSuccess,
	})
}
