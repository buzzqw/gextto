package backoff

import "testing"

func TestEscalationAndRecovery(t *testing.T) {
	if NextLevel(0, nil) != 1 {
		t.Fatal("first failure should start at level 1")
	}
	if PeriodSecs(1) != 60 || PeriodSecs(2) != 300 {
		t.Fatal("periods mismatch")
	}
	older := int64(120)
	if NextLevel(1, &older) != 2 {
		t.Fatal("older failure should escalate")
	}
	rapid := int64(10)
	if NextLevel(1, &rapid) != 1 {
		t.Fatal("rapid retry should keep the level")
	}
	max := int64(999999)
	if NextLevel(MaxLevel(), &max) != MaxLevel() {
		t.Fatal("level should cap")
	}
	if SuccessLevel(3) != 2 || SuccessLevel(1) != 0 || SuccessLevel(0) != 0 {
		t.Fatal("success should step down to zero")
	}
}
