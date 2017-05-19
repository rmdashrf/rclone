# WARNING
Don't use this. This is a hack because rclone's API keys were revoked and I needed
to copy off my files, desperately.

# How to use
1. Get [EditThisCookie](https://chrome.google.com/webstore/detail/editthiscookie/fngmhnnpilhplaeedifhccceomclgfbg?hl=en)
2. Log into Amazon Drive through the web client.
3. Export your cookies into a file (e.g. `acd_cookies.txt`)
4. Set environment variable `ACD_COOKIEHACK` to the path to the exported cookies.
5. Pray that Amazon doesn't ban you (or implements any form of validation) (or your session expires) (or anything)

# Example
```
export ACD_COOKIEHACK=/tmp/acd_cookies.txt
rclone_acd_hack copy acd:/ gdrive:
```
