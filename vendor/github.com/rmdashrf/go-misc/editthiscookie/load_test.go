package editthiscookie

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	fname := os.Getenv("EDITTHISCOOKIE_TESTFILE")
	t.Logf("Using testfile: %s\n", fname)
	if fname == "" {
		t.Skip()
		return
	}

	f, err := os.Open(fname)
	if err != nil {
		t.Fatal(err)
	}

	var cookies []Entry
	if err := json.NewDecoder(f).Decode(&cookies); err != nil {
		t.Fatal(err)
	}

	for _, c := range cookies {
		goCookie := c.GoCookie()
		t.Logf("%s\n", goCookie)
	}
}
