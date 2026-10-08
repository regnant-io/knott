#!/usr/bin/env python3
import sys
import re

# Read commit message from stdin
lines = sys.stdin.read().split('\n')

# Filter out Co-Authored-By lines containing "Claude"
filtered = [line for line in lines if not re.search(r'Co-Authored-By.*[Cc]laude', line)]

# Print filtered message
print('\n'.join(filtered))
