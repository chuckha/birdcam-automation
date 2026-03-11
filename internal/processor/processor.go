package processor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNoBirds is returned when detect_birds.py finds no activity in a video.
var ErrNoBirds = errors.New("no bird activity detected")

type Processor struct {
	pythonPath string
	scriptPath string
}

func New(pythonPath, scriptPath string) *Processor {
	return &Processor{pythonPath: pythonPath, scriptPath: scriptPath}
}

// Highlights runs detect_birds.py on the day and night video files,
// then concatenates the results into outFile using ffmpeg.
// If neither video has bird activity, returns ErrNoBirds.
func (p *Processor) Highlights(ctx context.Context, dayFile, nightFile, outFile string) error {
	dir := filepath.Dir(outFile)
	dayHighlights := filepath.Join(dir, "day_highlights.mp4")
	nightHighlights := filepath.Join(dir, "night_highlights.mp4")

	dayErr := p.runDetect(ctx, dayFile, dayHighlights)
	if dayErr != nil && !errors.Is(dayErr, ErrNoBirds) {
		return fmt.Errorf("detecting birds in day video: %w", dayErr)
	}

	nightErr := p.runDetect(ctx, nightFile, nightHighlights)
	if nightErr != nil && !errors.Is(nightErr, ErrNoBirds) {
		return fmt.Errorf("detecting birds in night video: %w", nightErr)
	}

	if errors.Is(dayErr, ErrNoBirds) && errors.Is(nightErr, ErrNoBirds) {
		return ErrNoBirds
	}

	// Concat whichever highlights were produced.
	var inputs []string
	if dayErr == nil {
		inputs = append(inputs, dayHighlights)
	}
	if nightErr == nil {
		inputs = append(inputs, nightHighlights)
	}

	if len(inputs) == 1 {
		// Only one file, just rename it.
		return os.Rename(inputs[0], outFile)
	}

	return p.concat(ctx, outFile, inputs)
}

// ProcessSingle runs bird detection on a single video file and writes
// the highlights to outFile. Returns ErrNoBirds if no activity is found.
func (p *Processor) ProcessSingle(ctx context.Context, videoFile, outFile string) error {
	return p.runDetect(ctx, videoFile, outFile)
}

func (p *Processor) runDetect(ctx context.Context, videoFile, outFile string) error {
	cmd := exec.CommandContext(ctx, p.pythonPath, p.scriptPath, videoFile, outFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			return ErrNoBirds
		}
		return fmt.Errorf("detect_birds.py failed for %s: %s: %w", videoFile, string(output), err)
	}
	return nil
}

func (p *Processor) concat(ctx context.Context, outFile string, inputs []string) error {
	dir := filepath.Dir(outFile)
	listFile := filepath.Join(dir, "concat_list.txt")

	var b strings.Builder
	for _, f := range inputs {
		fmt.Fprintf(&b, "file '%s'\n", f)
	}
	if err := os.WriteFile(listFile, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("writing concat list: %w", err)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listFile, "-c", "copy", outFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg concat failed: %s: %w", string(output), err)
	}
	return nil
}

// BuildDetectArgs returns the command arguments for a single detect_birds.py run.
func (p *Processor) BuildDetectArgs(videoFile, outFile string) []string {
	return []string{p.pythonPath, p.scriptPath, videoFile, outFile}
}
