# Contributing

Contributions are welcome as focused pull requests. Run 'task ci' on Linux and
'task regenerate-producers' on macOS arm64 with Python 3.13 when changing
producer-bound bytes. Never regenerate the macOS AGT lock on another platform.

Maintainer review is the sole publication decision. Automated and AI-assisted
review can inform that decision but is not a second human approval. Do not add
publication tooling to the immutable checkpoint or alter frozen evidence,
identifier, dependency, or platform pins without an explicit contract change.
