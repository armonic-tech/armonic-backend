package user

import "testing"

func TestSetVoiceState(t *testing.T) {
	cases := []struct {
		name                string
		muted, deafened     bool
		wantMuted, wantDeaf bool
	}{
		{"neither", false, false, false, false},
		{"muted only", true, false, true, false},
		{"deafened implies muted", false, true, true, true},
		{"both", true, true, true, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := &User{}
			gotMuted, gotDeaf := u.SetVoiceState(c.muted, c.deafened)
			if gotMuted != c.wantMuted || gotDeaf != c.wantDeaf {
				t.Fatalf("SetVoiceState(%v,%v) = (%v,%v), want (%v,%v)",
					c.muted, c.deafened, gotMuted, gotDeaf, c.wantMuted, c.wantDeaf)
			}
			if u.IsMuted() != c.wantMuted {
				t.Errorf("IsMuted() = %v, want %v", u.IsMuted(), c.wantMuted)
			}
			if u.IsDeafened() != c.wantDeaf {
				t.Errorf("IsDeafened() = %v, want %v", u.IsDeafened(), c.wantDeaf)
			}
		})
	}
}

func TestSetVoiceState_ClearsPrevious(t *testing.T) {
	u := &User{}
	u.SetVoiceState(true, true)
	if !u.IsMuted() || !u.IsDeafened() {
		t.Fatal("expected muted+deafened after first set")
	}
	u.SetVoiceState(false, false)
	if u.IsMuted() || u.IsDeafened() {
		t.Fatal("expected cleared after second set")
	}
}
