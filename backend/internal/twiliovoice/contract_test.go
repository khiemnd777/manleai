package twiliovoice

import "testing"

func TestCanonicalPathsKeepOneOpaqueRouteIdentity(t *testing.T) {
	paths := CanonicalPaths("3f17f690-7de4-4b26-91b8-2763ca15489d")
	if paths.Incoming != "/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/incoming" {
		t.Fatalf("incoming path=%q", paths.Incoming)
	}
	if paths.StreamStatus != "/api/voice/twilio/3f17f690-7de4-4b26-91b8-2763ca15489d/stream/status" {
		t.Fatalf("stream status path=%q", paths.StreamStatus)
	}
}

func TestProductionVoiceContractValidation(t *testing.T) {
	if !ValidE164("+13125550123") || ValidE164("3125550123") || ValidE164("+0123") {
		t.Fatal("E.164 validation contract changed")
	}
	if !ValidAccountSID("AC00000000000000000000000000000000") || ValidAccountSID("AC123") {
		t.Fatal("Account SID validation contract changed")
	}
	if !ValidPublicHTTPSBase("https://api.example.com") || ValidPublicHTTPSBase("http://api.example.com") ||
		ValidPublicHTTPSBase("https://api.example.com/base") || ValidPublicHTTPSBase("https://operator@api.example.com") {
		t.Fatal("public HTTPS base validation contract changed")
	}
}
