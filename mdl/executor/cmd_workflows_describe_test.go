// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/sdk/workflows"
)

// formatSingleActivity is a test helper that wraps a single activity in a Flow
// and runs formatWorkflowActivities to get the DESCRIBE output.
func formatSingleActivity(act workflows.WorkflowActivity, indent string) []string {
	flow := &workflows.Flow{
		Activities: []workflows.WorkflowActivity{act},
	}
	return formatWorkflowActivities(flow, indent)
}

// --- P0: strip Module.Microflow prefix from parameter names ---

func TestFormatCallMicroflowTask_ParameterNameStripping(t *testing.T) {
	tests := []struct {
		name          string
		paramName     string
		wantParamName string
	}{
		{
			name:          "three-segment name strips prefix",
			paramName:     "WorkflowBaseline.CallMF.Entity",
			wantParamName: "Entity",
		},
		{
			name:          "two-segment name strips prefix",
			paramName:     "CallMF.Entity",
			wantParamName: "Entity",
		},
		{
			name:          "single-segment name unchanged",
			paramName:     "Entity",
			wantParamName: "Entity",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &workflows.CallMicroflowTask{
				Microflow: "Module.SomeMicroflow",
				ParameterMappings: []*workflows.ParameterMapping{
					{Parameter: tc.paramName, Expression: "$WorkflowContext"},
				},
			}
			task.Name = "callMfTask"
			task.Caption = "Call MF"

			lines := formatCallMicroflowTask(task, "  ")
			output := strings.Join(lines, "\n")

			wantFragment := tc.wantParamName + " = "
			if !strings.Contains(output, wantFragment) {
				t.Errorf("expected output to contain %q, got:\n%s", wantFragment, output)
			}

			// Ensure the full qualified prefix is NOT in the parameter position
			if tc.paramName != tc.wantParamName && strings.Contains(output, tc.paramName+" = ") {
				t.Errorf("output should not contain full qualified name %q as parameter, got:\n%s", tc.paramName, output)
			}
		})
	}
}

// --- P2a: DESCRIBE outputs Caption with COMMENT 'caption' format ---

func TestFormatJumpTo_CaptionCommentFormat(t *testing.T) {
	tests := []struct {
		name    string
		caption string
		actName string
		want    string
	}{
		{
			name:    "caption used over name",
			caption: "Go Back to Review",
			actName: "jumpAct1",
			want:    "jump to target1 comment 'Go Back to Review'",
		},
		{
			// Was: fell back to the activity name and rendered
			// `comment 'jumpAct1'`. That comment was never authored — echoing it
			// made a plain `jump to X;` round-trip as `jump to X comment '…'`
			// (issuetracker #16). An absent caption must emit no comment clause.
			name:    "no comment clause when caption empty",
			caption: "",
			actName: "jumpAct1",
			want:    "jump to target1;",
		},
		{
			// buildJumpTo defaults Caption to the TARGET name, which is the exact
			// shape issuetracker #16 reported. It carries no authored information,
			// so it must not be echoed either.
			name:    "no comment clause when caption is the derived target name",
			caption: "target1",
			actName: "jumpAct2",
			want:    "jump to target1;",
		},
		{
			name:    "caption with single quote escaped",
			caption: "it's done",
			actName: "jumpAct1",
			want:    "jump to target1 comment 'it''s done'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			activity := &workflows.JumpToActivity{
				TargetActivity: "target1",
			}
			activity.Name = tc.actName
			activity.Caption = tc.caption

			lines := formatSingleActivity(activity, "")
			output := strings.Join(lines, "\n")

			if !strings.Contains(output, tc.want) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.want, output)
			}
		})
	}
}

func TestFormatWaitForTimer_CaptionCommentFormat(t *testing.T) {
	tests := []struct {
		name    string
		caption string
		actName string
		delay   string
		want    string
	}{
		{
			name:    "caption with delay",
			caption: "Wait 2 Hours",
			actName: "waitAct1",
			delay:   "${PT2H}",
			want:    "wait for timer '${PT2H}' comment 'Wait 2 Hours'",
		},
		{
			name:    "name fallback no delay",
			caption: "",
			actName: "waitAct1",
			delay:   "",
			want:    "wait for timer comment 'waitAct1'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			activity := &workflows.WaitForTimerActivity{
				DelayExpression: tc.delay,
			}
			activity.Name = tc.actName
			activity.Caption = tc.caption

			lines := formatSingleActivity(activity, "")
			output := strings.Join(lines, "\n")

			if !strings.Contains(output, tc.want) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.want, output)
			}
		})
	}
}

func TestFormatCallWorkflowActivity_CaptionCommentFormat(t *testing.T) {
	tests := []struct {
		name    string
		caption string
		actName string
		want    string
	}{
		{
			name:    "caption used",
			caption: "Run Sub-Workflow",
			actName: "callWf1",
			want:    "call workflow Module.SubFlow comment 'Run Sub-Workflow'",
		},
		{
			name:    "name fallback",
			caption: "",
			actName: "callWf1",
			want:    "call workflow Module.SubFlow comment 'callWf1'",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			activity := &workflows.CallWorkflowActivity{
				Workflow: "Module.SubFlow",
			}
			activity.Name = tc.actName
			activity.Caption = tc.caption

			lines := formatCallWorkflowActivity(activity, "")
			output := strings.Join(lines, "\n")

			if !strings.Contains(output, tc.want) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.want, output)
			}
		})
	}
}
