# Install bino

The extension runs the `bino` command line tool for previews, builds and editor help.

It looks for bino in this order:

1. the `bino.binPath` setting,
2. `bino` on your `PATH`,
3. a bino that the extension downloaded for you.

**Check for bino** runs `bino version`. This step is done once bino is found.

**Download bino** gets the latest release from GitHub, checks its SHA-256 checksum and keeps it in the extension's storage folder. Nothing is downloaded unless you click it.

You can also install bino yourself: see [the installation guide](https://cli.bino.bi/getting-started/installation/).
