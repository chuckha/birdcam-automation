#!/usr/bin/env python3
"""Detect bird visits in a birdhouse video by monitoring the entrance zone.

Birds enter and exit through the top-center of the frame (top-down camera
looking into a birdhouse). We watch a trigger zone around the entrance for
significant change vs. a reference "empty" frame. A state machine tracks
EMPTY → OCCUPIED → EMPTY transitions to identify individual visits.

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

SAMPLE_FPS = 2

# Trigger zone: top-center rectangle where the bird enters/exits.
# Expressed as fractions of frame dimensions.
ZONE_TOP = 0.0
ZONE_BOTTOM = 0.12   # top 12% of frame — just the entrance hole
ZONE_LEFT = 0.25
ZONE_RIGHT = 0.75    # middle 50% of width

# How many reference frames to average for the "empty" baseline.
REFERENCE_FRAME_COUNT = 5

# Fraction of pixels in the trigger zone that must differ from the reference
# to count as "disturbed" (bird passing through).
ZONE_DIFF_THRESHOLD = 25   # per-pixel intensity difference
ZONE_DISTURBED_FRACTION = 0.08  # 8% of zone pixels must change

# State machine parameters (in seconds, since we sample at SAMPLE_FPS).
ENTER_HOLD = 1    # zone must be disturbed for this many seconds to confirm entry
EXIT_HOLD = 3     # zone must be clear for this many seconds to confirm exit
ENTRY_COOLDOWN = 5   # seconds after entry before we start looking for exits
SETTLE_TIME = 5      # seconds the zone must be still after exit before we resume entry detection

EXTEND_BEFORE = 3  # seconds of context before a visit
EXTEND_AFTER = 3   # seconds of context after a visit
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


def get_zone(gray):
    """Extract the trigger zone from a grayscale frame."""
    h, w = gray.shape
    y1, y2 = int(h * ZONE_TOP), int(h * ZONE_BOTTOM)
    x1, x2 = int(w * ZONE_LEFT), int(w * ZONE_RIGHT)
    return gray[y1:y2, x1:x2]


def build_reference(frames):
    """Average the first N frames' trigger zones to build an empty baseline."""
    zones = []
    for frame_path in frames[:REFERENCE_FRAME_COUNT]:
        gray = cv2.imread(str(frame_path), cv2.IMREAD_GRAYSCALE)
        gray = cv2.GaussianBlur(gray, (21, 21), 0)
        zones.append(get_zone(gray).astype(np.float32))
    return np.mean(zones, axis=0).astype(np.uint8)


def detect_visits(frames_dir, diag_csv=None):
    """Detect bird visits by monitoring the entrance zone.

    Returns a list of (enter_second, exit_second) tuples.
    If diag_csv is a path, writes per-frame diagnostics there.
    """
    frames = sorted(frames_dir.glob("frame_*.jpg"))
    if len(frames) < REFERENCE_FRAME_COUNT + 1:
        return []

    reference = build_reference(frames)
    zone_pixels = reference.shape[0] * reference.shape[1]

    # State machine
    STATE_EMPTY = "empty"
    STATE_OCCUPIED = "occupied"
    STATE_SETTLING = "settling"  # post-exit: waiting for zone to be still
    state = STATE_EMPTY

    # Track how long the zone has been in a particular condition
    disturbed_count = 0  # consecutive seconds the zone has been disturbed
    clear_count = 0      # consecutive seconds the zone has been clear
    settle_count = 0     # consecutive frames with no f2f motion while settling

    visits = []
    enter_time = None
    prev_zone = None

    diag_file = None
    if diag_csv is not None:
        diag_file = open(diag_csv, "w")
        diag_file.write("time_s,ref_fraction,f2f_fraction,state\n")

    for i, frame_path in enumerate(frames):
        t = i / SAMPLE_FPS  # time in seconds

        gray = cv2.imread(str(frame_path), cv2.IMREAD_GRAYSCALE)
        gray = cv2.GaussianBlur(gray, (21, 21), 0)
        zone = get_zone(gray)

        # Compare against reference (for entry detection)
        ref_diff = cv2.absdiff(zone, reference)
        ref_fraction = np.count_nonzero(ref_diff > ZONE_DIFF_THRESHOLD) / zone_pixels
        ref_disturbed = ref_fraction >= ZONE_DISTURBED_FRACTION

        # Frame-to-frame difference in the zone (for exit detection)
        if prev_zone is not None:
            f2f_diff = cv2.absdiff(zone, prev_zone)
            f2f_fraction = np.count_nonzero(f2f_diff > ZONE_DIFF_THRESHOLD) / zone_pixels
        else:
            f2f_fraction = 0.0
        f2f_motion = f2f_fraction >= ZONE_DISTURBED_FRACTION

        if diag_file is not None:
            diag_file.write(f"{t:.1f},{ref_fraction:.4f},{f2f_fraction:.4f},{state}\n")

        if state == STATE_SETTLING:
            # After an exit, wait for the zone to be still (no f2f motion)
            # before snapshotting a new reference and resuming entry detection.
            # Update the visit's exit time so the segment includes the full
            # departure (bird perching on the hole, etc).
            if f2f_motion:
                settle_count = 0
            else:
                settle_count += 1
                if settle_count >= SETTLE_TIME * SAMPLE_FPS:
                    # Extend the last visit's exit to when settling finished
                    settle_end = t - SETTLE_TIME
                    old_start, _ = visits[-1]
                    visits[-1] = (old_start, settle_end)
                    reference = zone.copy()
                    state = STATE_EMPTY
                    disturbed_count = 0

        elif state == STATE_EMPTY:
            if ref_disturbed:
                disturbed_count += 1
                if disturbed_count >= ENTER_HOLD * SAMPLE_FPS:
                    state = STATE_OCCUPIED
                    enter_time = t - ENTER_HOLD
                    clear_count = 0
                    exit_motion_seen = False
                    print(f"  Bird entered at {enter_time:.1f}s")
                # Don't update reference while disturbance is building —
                # a fast entry could get absorbed if we keep updating.
            else:
                disturbed_count = 0
                # Continuously update reference while empty so gradual
                # lighting changes and nest shifts don't accumulate.
                reference = zone.copy()

        elif state == STATE_OCCUPIED:
            # Exit detection: look for a burst of frame-to-frame motion
            # (the bird flying out), then the zone settling down (no more
            # frame-to-frame changes) which means exit is complete.
            # Skip exit detection during cooldown so entry motion isn't
            # mistaken for an exit.
            if t - enter_time < ENTRY_COOLDOWN:
                pass
            elif f2f_motion:
                exit_motion_seen = True
                clear_count = 0
            elif exit_motion_seen:
                # Motion burst happened, now waiting for zone to settle
                clear_count += 1
                if clear_count >= EXIT_HOLD * SAMPLE_FPS:
                    exit_time = t - EXIT_HOLD
                    visits.append((enter_time, exit_time))
                    print(f"  Bird exited at {exit_time:.1f}s (visit: {exit_time - enter_time:.0f}s)")
                    state = STATE_SETTLING
                    settle_count = 0
                    disturbed_count = 0
                    enter_time = None
                    exit_motion_seen = False

        prev_zone = zone.copy()

        if (i + 1) % (60 * SAMPLE_FPS) == 0:
            mins = int(t) // 60
            print(f"  Processed {mins} minutes ({i + 1}/{len(frames)} frames)...")

    if diag_file is not None:
        diag_file.close()
        print(f"  Diagnostic CSV written to {diag_csv}")

    # If the bird is still inside at the end of the video, close the visit
    if state == STATE_OCCUPIED and enter_time is not None:
        exit_time = len(frames) / SAMPLE_FPS
        visits.append((enter_time, exit_time))
        print(f"  Bird still inside at end of video (visit: {exit_time - enter_time:.0f}s)")

    return visits


def visits_to_segments(visits):
    """Convert visit tuples to segments with context padding."""
    segments = []
    for enter_time, exit_time in visits:
        start = max(0, enter_time - EXTEND_BEFORE)
        end = exit_time + EXTEND_AFTER
        segments.append((int(start), int(end)))

    # Merge overlapping segments
    if not segments:
        return []
    merged = [segments[0]]
    for start, end in segments[1:]:
        prev_start, prev_end = merged[-1]
        if start <= prev_end:
            merged[-1] = (prev_start, max(prev_end, end))
        else:
            merged.append((start, end))
    return merged


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

    # Segments are encoded as mp4/h264. If the output container matches we
    # can stream-copy; otherwise we need to re-encode for the target format.
    output_ext = Path(output_path).suffix.lower()
    if output_ext in (".mp4", ".m4v", ".mov"):
        codec_args = ["-c", "copy"]
    else:
        codec_args = ["-c:v", "libx264", "-preset", "fast", "-crf", "18",
                      "-c:a", "aac", "-b:a", "128k"]

    # Always write to an mp4 temp file first, then produce the final output.
    tmp_output = work_dir / "merged.mp4"
    subprocess.run(
        [
            "ffmpeg", "-y",
            "-f", "concat", "-safe", "0",
            "-i", str(concat_list),
            "-c", "copy",
            str(tmp_output),
        ],
        check=True,
        capture_output=True,
    )

    if output_ext in (".mp4", ".m4v", ".mov"):
        shutil.move(str(tmp_output), str(output_path))
    else:
        subprocess.run(
            [
                "ffmpeg", "-y",
                "-i", str(tmp_output),
                "-c:v", "libx264", "-preset", "fast", "-crf", "18",
                "-c:a", "aac", "-b:a", "128k",
                str(output_path),
            ],
            check=True,
            capture_output=True,
        )


def process(video_path, output_path):
    """Detect bird visits in video_path and produce trimmed output_path.

    Returns True if visits were detected and output was produced, False otherwise.
    """
    video_path = Path(video_path)
    output_path = Path(output_path)
    work_dir = Path(tempfile.mkdtemp(prefix="birds_"))

    try:
        frames_dir = work_dir / "frames"

        print(f"Step 1: Extracting frames at {SAMPLE_FPS} fps...")
        extract_frames(video_path, frames_dir)

        print("Step 2: Detecting bird visits via entrance zone monitoring...")
        diag_csv = output_path.with_suffix(".diag.csv")
        visits = detect_visits(frames_dir, diag_csv=diag_csv)
        print(f"  Found {len(visits)} visits")

        if not visits:
            print("No visits detected!")
            return False

        segments = visits_to_segments(visits)
        print(f"  Merged into {len(segments)} segments:")
        for start, end in segments:
            mins, secs = divmod(start, 60)
            end_mins, end_secs = divmod(end, 60)
            print(f"    {mins:02d}:{secs:02d} - {end_mins:02d}:{end_secs:02d} ({end - start}s)")

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
