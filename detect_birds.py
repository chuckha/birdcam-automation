#!/usr/bin/env python3
"""Detect bird presence in a nest video using frame differencing and produce a trimmed video.

Usage:
    python3 detect_birds.py <input_video> <output_video>

Exit codes:
    0 — highlights produced
    1 — error
    2 — no bird activity detected
"""

import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

import cv2
import numpy as np
from PIL import Image, ImageDraw, ImageFont

SAMPLE_FPS = 1
DIFF_THRESHOLD = 25  # pixel intensity change to count as "different"
PIXEL_FRACTION = 0.02  # fraction of pixels that must differ to flag a frame
GAP_TOLERANCE = 2  # merge segments within this many seconds
EXTEND_BEFORE = 5  # seconds of context before motion
EXTEND_AFTER = 5  # seconds of context after motion
SKIP_DISPLAY_DURATION = 4  # seconds to show the skip overlay


def extract_frames(video_path, frames_dir):
    """Extract frames from the video at SAMPLE_FPS."""
    frames_dir.mkdir(exist_ok=True)
    subprocess.run(
        [
            "ffmpeg", "-y", "-i", str(video_path),
            "-vf", f"fps={SAMPLE_FPS}",
            str(frames_dir / "frame_%06d.jpg"),
        ],
        check=True,
        capture_output=True,
    )


def detect_bird_times(frames_dir):
    """Detect activity using frame-to-frame differencing.

    Compare each frame to the previous frame. A burst of changes means
    something moved (bird entering/moving/leaving). We extend each motion
    detection by a window to capture the bird sitting still between movements.
    """
    frames = sorted(frames_dir.glob("frame_*.jpg"))
    total_pixels = None
    prev_gray = None
    frame_diffs = []

    for i, frame_path in enumerate(frames):
        gray = cv2.imread(str(frame_path), cv2.IMREAD_GRAYSCALE)
        gray = cv2.GaussianBlur(gray, (21, 21), 0)

        if prev_gray is not None:
            diff = cv2.absdiff(prev_gray, gray)
            if total_pixels is None:
                total_pixels = gray.shape[0] * gray.shape[1]
            changed = np.count_nonzero(diff > DIFF_THRESHOLD)
            frame_diffs.append(changed / total_pixels)
        else:
            frame_diffs.append(0.0)

        prev_gray = gray

        if (i + 1) % 60 == 0:
            print(f"  Processed {i + 1}/{len(frames)} frames...")

    motion_frames = {i for i, d in enumerate(frame_diffs) if d >= PIXEL_FRACTION}
    print(f"  Raw motion detected in {len(motion_frames)} frames")

    active_frames = set()
    for f in motion_frames:
        for j in range(max(0, f - EXTEND_BEFORE), min(len(frames), f + EXTEND_AFTER + 1)):
            active_frames.add(j)

    return sorted(active_frames)


def build_segments(bird_seconds):
    """Merge individual seconds into contiguous segments with gap tolerance."""
    if not bird_seconds:
        return []

    segments = []
    start = bird_seconds[0]
    end = bird_seconds[0]

    for s in bird_seconds[1:]:
        if s <= end + GAP_TOLERANCE + 1:
            end = s
        else:
            segments.append((max(0, start - 1), end + 1))
            start = s
            end = s

    segments.append((max(0, start - 1), end + 1))
    return segments


def format_skip(seconds):
    """Format a duration as +MM:SS or +H:MM:SS."""
    mins, secs = divmod(seconds, 60)
    hours, mins = divmod(mins, 60)
    if hours:
        return f"+{hours:d}:{mins:02d}:{secs:02d}"
    return f"+{mins:02d}:{secs:02d}"


def load_font(size=32):
    """Load a monospace font, falling back gracefully across platforms."""
    candidates = [
        "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",  # Linux/Jetson
        "/System/Library/Fonts/Menlo.ttc",  # macOS
    ]
    for path in candidates:
        try:
            return ImageFont.truetype(path, size)
        except OSError:
            continue
    return ImageFont.load_default(size=size)


def make_overlay_png(text, filepath, video_width=960, video_height=720):
    """Create a transparent PNG with skip text positioned in the top-right."""
    img = Image.new("RGBA", (video_width, video_height), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    font = load_font()

    bbox = draw.textbbox((0, 0), text, font=font)
    tw = bbox[2] - bbox[0]
    x = video_width - tw - 20
    y = 20

    for dx in range(-2, 3):
        for dy in range(-2, 3):
            draw.text((x + dx, y + dy), text, font=font, fill=(0, 0, 0, 255))
    draw.text((x, y), text, font=font, fill=(255, 255, 255, 255))

    img.save(filepath)


def concat_segments(segments, video_path, output_path, work_dir):
    """Use ffmpeg to extract and concatenate bird segments."""
    segment_files = []
    overlay_files = []

    for i, (start, end) in enumerate(segments):
        seg_file = work_dir / f"seg_{i:03d}.mp4"
        overlay_png = None

        if i > 0:
            prev_end = segments[i - 1][1]
            gap = start - prev_end
            if gap > 0:
                overlay_png = work_dir / f"overlay_{i:03d}.png"
                make_overlay_png(format_skip(gap), str(overlay_png))
                overlay_files.append(overlay_png)

        if overlay_png:
            cmd = [
                "ffmpeg", "-y",
                "-ss", str(start),
                "-i", str(video_path),
                "-loop", "1", "-i", str(overlay_png),
                "-t", str(end - start),
                "-filter_complex",
                f"[1:v]format=rgba[ovr];[0:v][ovr]overlay=0:0:enable='between(t,0,{SKIP_DISPLAY_DURATION})'",
                "-c:v", "libx264", "-preset", "fast", "-crf", "18",
                "-c:a", "aac", "-b:a", "128k",
                str(seg_file),
            ]
        else:
            cmd = [
                "ffmpeg", "-y",
                "-ss", str(start),
                "-i", str(video_path),
                "-t", str(end - start),
                "-c:v", "libx264", "-preset", "fast", "-crf", "18",
                "-c:a", "aac", "-b:a", "128k",
                str(seg_file),
            ]

        subprocess.run(cmd, check=True, capture_output=True)
        segment_files.append(seg_file)

        gap_info = ""
        if i > 0:
            prev_end = segments[i - 1][1]
            gap = start - prev_end
            if gap > 0:
                gap_info = f" (skip: {format_skip(gap)})"
        print(f"  Encoded segment {i + 1}/{len(segments)}: {start}s - {end}s{gap_info}")

    concat_list = work_dir / "concat_list.txt"
    concat_list.write_text("\n".join(f"file '{f}'" for f in segment_files))

    subprocess.run(
        [
            "ffmpeg", "-y",
            "-f", "concat", "-safe", "0",
            "-i", str(concat_list),
            "-c", "copy",
            str(output_path),
        ],
        check=True,
        capture_output=True,
    )


def process(video_path, output_path):
    """Detect bird activity in video_path and produce trimmed output_path.

    Returns True if birds were detected and output was produced, False otherwise.
    """
    video_path = Path(video_path)
    output_path = Path(output_path)
    work_dir = Path(tempfile.mkdtemp(prefix="birds_"))

    try:
        frames_dir = work_dir / "frames"

        print(f"Step 1: Extracting frames at {SAMPLE_FPS} fps...")
        extract_frames(video_path, frames_dir)

        print("Step 2: Detecting birds via frame differencing...")
        bird_seconds = detect_bird_times(frames_dir)
        print(f"  Found activity in {len(bird_seconds)} seconds of video")

        segments = build_segments(bird_seconds)
        print(f"  Merged into {len(segments)} segments:")
        for start, end in segments:
            mins, secs = divmod(start, 60)
            end_mins, end_secs = divmod(end, 60)
            print(f"    {mins:02d}:{secs:02d} - {end_mins:02d}:{end_secs:02d} ({end - start}s)")

        if not segments:
            print("No activity detected!")
            return False

        print(f"Step 3: Concatenating {len(segments)} segments...")
        concat_segments(segments, video_path, output_path, work_dir)
        print(f"Done! Output: {output_path}")
        return True
    finally:
        shutil.rmtree(work_dir, ignore_errors=True)


if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} <input_video> <output_video>", file=sys.stderr)
        sys.exit(1)

    found = process(sys.argv[1], sys.argv[2])
    sys.exit(0 if found else 2)
