package cli

import "testing"

func TestUnescapeMount(t *testing.T) {
	if got := unescapeMount(`/media/me/My\040Card\134x`); got != `/media/me/My Card\x` {
		t.Errorf("%q", got)
	}
}
