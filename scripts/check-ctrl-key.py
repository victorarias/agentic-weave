#!/usr/bin/env python3
import sys
import termios
import tty

fd = sys.stdin.fileno()
old = termios.tcgetattr(fd)
try:
    tty.setraw(fd)
    print("Press one key/chord, then it will print the first byte immediately.")
    data = sys.stdin.buffer.read(1)
    print("hex:", data.hex())
    print("bytes:", list(data))
finally:
    termios.tcsetattr(fd, termios.TCSADRAIN, old)
