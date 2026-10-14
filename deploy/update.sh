#!/usr/bin/env bash
# Pull both repositories, rebuild and restart the API and website. The
# database container is left running; migrations run when the API starts.
set -euo pipefail
base=/opt/ourtaiko-database
for repo in backend frontend; do
  cd "$base/src/$repo"
  test -z "$(git status --porcelain)" || { echo "$repo checkout has local changes"; exit 1; }
  git pull --ff-only origin main
done
compose="sudo docker compose -f $base/src/backend/deploy/compose.yml"
$compose build api web
$compose up -d --wait
curl -fsS http://127.0.0.1:8090/readyz
echo
git -C "$base/src/backend" rev-parse HEAD
git -C "$base/src/frontend" rev-parse HEAD
