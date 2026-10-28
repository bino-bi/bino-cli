---
description: Check this bino package against the registry and publish a new version after the human confirms.
argument-hint: "[patch|minor|major]"
---

Publish the current bino package. Optional bump: **$ARGUMENTS**

Follow `bino-packaging`. Publishing is irreversible, so the human confirms the exact publish.

1. `validate_project()`. Stop on any error; fix package findings first.
2. `registry_auth_status()`. If not authenticated, tell the human to run `bino registry login` and
   stop.
3. Pick the bump: use the argument, or propose one from the changes (see `bino-packaging`) and let the
   human choose.
4. `registry_publish_check(bump)`. On a rejection, show the gate findings and fix them; repeat at most
   twice, then hand the rest to the human.
5. Show the human the package, the version it would mint, the file list, and on a first publish the
   visibility. Ask: publish now?
6. Only on yes: call `registry_publish(bump, visibility?)` if the server offers it. Otherwise give the
   human the command `bino publish --bump <bump>` (the tool needs `bino mcp --allow-publish`, or
   `bino daemon --mcp-allow-publish` when a daemon runs).
7. Report the minted version and digest, or `unchanged` when the content was already published.
