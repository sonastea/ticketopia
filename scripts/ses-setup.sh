#!/bin/sh
# Local Compose fixtures only. Never source .env or forward live AWS credentials.
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"

if ! command -v docker >/dev/null 2>&1; then
  echo "make ses-setup requires docker" >&2
  exit 1
fi
compose() {
  # Ignore the app's .env; MiniStack needs no account/auth token.
  docker compose --env-file /dev/null -f "$root/deploy/ministack/compose.yaml" "$@"
}
local_aws() {
  compose exec -T \
    -e AWS_ACCESS_KEY_ID=test -e AWS_SECRET_ACCESS_KEY=test -e AWS_SESSION_TOKEN= \
    -e AWS_EC2_METADATA_DISABLED=true \
    -e AWS_IGNORE_CONFIGURED_ENDPOINT_URLS=true \
    ministack awslocal --endpoint-url http://localhost:4566 --region us-east-1 "$@"
}

echo "Preparing local SES capture in MiniStack (localhost:4566)..."
if ! compose up -d --wait --wait-timeout 180 ministack; then
  echo "MiniStack did not become SESv2-ready. Check port 4566 and run: docker compose --env-file /dev/null -f deploy/ministack/compose.yaml logs --tail 40 ministack" >&2
  exit 1
fi

identity=notifications-dev@ticketopia.sonastea.com
if ! local_aws sesv2 get-email-identity --email-identity "$identity" >/dev/null 2>&1; then
  local_aws sesv2 create-email-identity --email-identity "$identity" >/dev/null
fi
verified=$(local_aws sesv2 get-email-identity --email-identity "$identity" --query VerifiedForSendingStatus --output text)
case "$verified" in
  True|true) ;;
  *) echo "MiniStack sender is not verified; check SES identity setup." >&2; exit 1 ;;
esac

echo "Local SES sender is ready; messages are captured, not delivered."
echo "Set SES_ENDPOINT_URL=http://localhost:4566 and REMINDERS_EMAIL_FROM=Ticketopia DEV <notifications-dev@ticketopia.sonastea.com> in your local app environment."
echo "Inspect captures at http://localhost:4566/_ministack/ses/messages. See docs/local-development.md for complete settings."
