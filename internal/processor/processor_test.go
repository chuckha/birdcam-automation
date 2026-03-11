package processor

import (
	"testing"
)

func TestProcessor_BuildDetectArgs(t *testing.T) {
	p := New("python3", "/opt/detect_birds.py")
	args := p.BuildDetectArgs("/data/day.mp4", "/data/day_highlights.mp4")

	expected := []string{"python3", "/opt/detect_birds.py", "/data/day.mp4", "/data/day_highlights.mp4"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d", len(expected), len(args))
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("arg[%d]: expected %q, got %q", i, expected[i], a)
		}
	}
}
