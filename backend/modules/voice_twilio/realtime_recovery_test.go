package voice_twilio

import (
	"strings"
	"testing"
)

func TestRealtimeInputRecoveryProgressesAndResets(t *testing.T) {
	recovery := realtimeInputRecovery{}
	prompt := "What day works for you?"

	first := recovery.Reject(prompt)
	if first.Streak != 1 || first.Action != realtimeRecoveryRepeatShort || first.Handoff {
		t.Fatalf("first decision = %#v", first)
	}
	second := recovery.Reject(prompt)
	if second.Streak != 2 || second.Action != realtimeRecoveryRepeatScoped || !strings.Contains(second.Message, prompt) {
		t.Fatalf("second decision = %#v", second)
	}
	third := recovery.Reject(prompt)
	if third.Streak != 3 || third.Action != realtimeRecoveryNoiseCoach || !strings.Contains(third.Message, "somewhere quieter") {
		t.Fatalf("third decision = %#v", third)
	}
	fourth := recovery.Reject(prompt)
	if fourth.Streak != realtimeInputHandoffThreshold || fourth.Action != realtimeRecoveryOwnerHandoff || !fourth.Handoff {
		t.Fatalf("fourth decision = %#v", fourth)
	}

	recovery.Reset()
	reset := recovery.Reject("What time works for you?")
	if reset.Streak != 1 || reset.Action != realtimeRecoveryRepeatShort {
		t.Fatalf("reset decision = %#v", reset)
	}
}
