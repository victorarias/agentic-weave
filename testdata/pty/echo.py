#!/usr/bin/env python3
import sys

while True:
    data = sys.stdin.buffer.read1(4096)
    if not data:
        break
    sys.stdout.buffer.write(data)
    sys.stdout.buffer.flush()
