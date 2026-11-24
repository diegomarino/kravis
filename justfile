# Install the repository's git hooks.
hooks:
    git config core.hooksPath "{{justfile_directory()}}/.githooks"

# Check the ddduck product model and its generated views.
check:
    ddduck check --root docs/ddd
