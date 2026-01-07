#!/usr/bin/env python3
import struct
import sys

filename = sys.argv[1] if len(sys.argv) > 1 else "recorded_audio.wav"

with open(filename, "r+b") as f:
    # Get file size
    f.seek(0, 2)  # Seek to end
    file_size = f.tell()

    # Calculate data size (file size - 44 byte header)
    data_size = file_size - 44

    # Update RIFF chunk size (file size - 8)
    f.seek(4)
    f.write(struct.pack('<I', file_size - 8))

    # Update data chunk size
    f.seek(40)
    f.write(struct.pack('<I', data_size))

    print(f"Fixed WAV header:")
    print(f"  File size: {file_size} bytes")
    print(f"  Data size: {data_size} bytes")
    print(f"  Duration: {data_size / (16000 * 2):.2f} seconds")
