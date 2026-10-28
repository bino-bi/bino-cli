---
description: Show the live preview of the report or of one component, page or artefact.
argument-hint: "[name]"
---

Show the bino preview. Optional document name: **$ARGUMENTS**

1. Call `preview_server_start(name?)` with the name from the arguments, if any. It starts
   `bino preview` without the system browser, or reuses the preview this server already started,
   and waits for the first render. With a name it also returns `embedding_url`: only that document,
   without the preview UI. On a tool error, show its reason. Ask for a name only when the error lists
   the available names; a failed first render needs `validate_project()` instead. If another
   preview (e.g. one started in a terminal) holds the port, pass a different `port`.
2. Pick the URL to show: `embedding_url` when a name was given, else `url` (the full preview with
   its page and artefact navigation).
3. The URL is loopback (`127.0.0.1`) on the machine that runs bino. Only a browser on that same
   machine can load it. **If you can drive such a browser** (in a local Claude desktop session: the
   built-in browser), open the URL there in a new tab, take a screenshot, and say in one or two lines
   what you see.
4. **Otherwise**, if the human works on the machine that runs bino, give them the URL. If bino runs
   somewhere they cannot reach (a cloud session), say that the preview cannot be shown from here.
5. `ready=false` means the first render did not finish within the wait; the page shows a loading
   screen until it does. Stop the server with `preview_server_stop` only when the human asks.
