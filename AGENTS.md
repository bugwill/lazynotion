# Project working agreements

## Secret configuration: direct access prohibition

- Never directly access `~/.config/lazynotion/config.toml`. It contains authentication keys.
- This includes reading, previewing, searching, inspecting metadata, copying,
  transmitting, modifying, or deleting the file. Do not use scripts,
  subprocesses, or symlinks to bypass this prohibition.
- Exception: launching `lazynotion` through the shell for real application
  testing is permitted. The application may load its configuration normally.
  Observe only application behavior and UI output; never extract or expose
  configuration contents or authentication keys. This exception does not
  authorize authentication commands or changes to the configuration.
- Exclude this file from recursive searches, scans, archives, and backups.
- Automated unit tests must use isolated temporary configuration directories
  and dummy tokens. Interactive application tests may use the shell-launch
  exception above.

## Other sensitive files

- Never access SSH keys, public keys, certificates, or their copies. Exclude
  `.ssh` directories and known or suspected SSH key files from searches.
- Never read, copy, transmit, modify, or delete
  `/home/ubuntu/Documents/ConfigSync/UbuntuServer/Cron.env` or
  `/home/ubuntu/Documents/ConfigSync/UbuntuServer/.secret.env`, directly or
  indirectly.

## Build cleanup

- Clean up temporary build files, directories, and logs after they are no longer
  required, on both successful and failed builds.
- Verify exact paths and ownership before deleting anything. Keep shared caches,
  pre-existing files, and artifacts needed for active debugging or deliverables.
