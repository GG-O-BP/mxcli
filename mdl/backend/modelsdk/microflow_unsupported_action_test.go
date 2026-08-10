// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"github.com/mendixlabs/mxcli/modelsdk/codec"
	"github.com/mendixlabs/mxcli/sdk/microflows"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// upstream #863: an action with no reader mapping produced a nil Action, which
// DESCRIBE rendered as the anonymous "-- Empty action" — indistinguishable from
// an activity that genuinely has no action, and stripped of the
// ErrorHandlingType that keeps its error branch attached.
//
// decodeAction returns the raw action; the UnsupportedAction stand-in is built
// one level up, in activityFromGen, so this asserts the reader still declines to
// map it while errorHandlingTypeOf recovers the field generically.
func TestUnsupportedAction_ErrorHandlingTypeIsRecovered(t *testing.T) {
	tests := []struct {
		storageType string
		stored      string
		want        string
	}{
		{"Microflows$SynchronizeAction", "CustomWithoutRollback", "CustomWithoutRollback"},
		{"Microflows$WorkflowCallAction", "Custom", "Custom"},
		{"Microflows$SendEmailAction", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.storageType, func(t *testing.T) {
			doc := bson.D{
				{Key: "$ID", Value: "a-1"},
				{Key: "$Type", Value: tc.storageType},
			}
			if tc.stored != "" {
				doc = append(doc, bson.E{Key: "ErrorHandlingType", Value: tc.stored})
			}
			el, err := codec.NewDecoder(codec.DefaultRegistry).Decode(mustMarshalFlow(doc))
			if err != nil {
				t.Fatalf("decode action: %v", err)
			}
			if got := errorHandlingTypeOf(el); got != tc.want {
				t.Errorf("errorHandlingTypeOf = %q, want %q", got, tc.want)
			}
		})
	}
}

// The stand-in must never reach BSON: the reader kept only the type name and the
// error-handling type, so writing it back would produce an activity stripped of
// every other property. microflowActionToGen has no case for it, so it maps to
// nil — asserted here so a future "helpful" case cannot silently start writing
// truncated actions.
func TestUnsupportedAction_IsNotWritable(t *testing.T) {
	act := &microflows.UnsupportedAction{StorageType: "Microflows$SynchronizeAction"}
	if got := microflowActionToGen(act); got != nil {
		t.Fatalf("microflowActionToGen(UnsupportedAction) = %T, want nil — "+
			"writing the stand-in back would emit an action stripped of its real properties", got)
	}
}
