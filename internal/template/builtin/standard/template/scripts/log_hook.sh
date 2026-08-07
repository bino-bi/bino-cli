#!/usr/bin/env bash
echo "[$BINO_HOOK] mode=$BINO_MODE artefact=${BINO_ARTEFACT_NAME:-} kind=${BINO_ARTEFACT_KIND:-}" >> "$BINO_WORKDIR/dist/log.txt"
