# shellcheck shell=bash
# tests/terraform/scripts/lib.sh
#
# Shared helpers sourced by run.sh and the assertion scripts. Two
# things live here: ssm_run (synchronous remote command execution
# without SSH) and small terminal-output utilities.

# ssm_run executes a shell snippet on an EC2 instance via SSM
# send-command and blocks until completion. Echoes stdout and exits
# non-zero if the remote command failed or timed out.
#
# Usage:  ssm_run <instance-id> <timeout-seconds> <<'EOF'
#           ... shell ...
#         EOF
ssm_run() {
  local instance_id="$1"
  local timeout="$2"
  # Optional 3rd arg overrides $AWS_REGION — required for multi-
  # region configs whose instance_targets list spans regions. The
  # SSM API is regional and rejects an InstanceId from a different
  # region with InvalidInstanceId.
  local region="${3:-$AWS_REGION}"
  local script
  script=$(cat)

  # Build the entire request as JSON via jq and pass via
  # --cli-input-json. The CLI shorthand
  #   --parameters "commands=[<json-string>]"
  # mangles multi-line scripts (embedded \n / \\ and * sequences
  # leak through the shorthand parser and drop flag values silently);
  # JSON round-trips cleanly.
  local input_json
  input_json=$(jq -n \
    --arg iid "$instance_id" \
    --arg cmd "$script" \
    --argjson timeout "$timeout" \
    '{
      InstanceIds: [$iid],
      DocumentName: "AWS-RunShellScript",
      Comment: "outrelay-smoke",
      TimeoutSeconds: $timeout,
      Parameters: { commands: [$cmd] }
    }')

  local cmd_id
  cmd_id=$(aws ssm send-command \
    --region "$region" \
    --cli-input-json "$input_json" \
    --query 'Command.CommandId' --output text)

  # Poll until the invocation reaches a terminal state. SSM rejects
  # GetCommandInvocation immediately after send-command; sleep once
  # before the first read.
  sleep 2
  local end=$(( $(date +%s) + timeout ))
  while :; do
    local status
    status=$(aws ssm get-command-invocation \
      --region "$region" \
      --command-id "$cmd_id" \
      --instance-id "$instance_id" \
      --query 'Status' --output text 2>/dev/null || echo "Pending")
    case "$status" in
      Success) break ;;
      Failed|Cancelled|TimedOut)
        aws ssm get-command-invocation \
          --region "$region" \
          --command-id "$cmd_id" \
          --instance-id "$instance_id" \
          --query '{stdout:StandardOutputContent,stderr:StandardErrorContent}' \
          --output json >&2
        return 1
        ;;
    esac
    if [[ $(date +%s) -ge $end ]]; then
      echo "ssm_run: timed out after ${timeout}s on $instance_id" >&2
      return 1
    fi
    sleep 2
  done

  aws ssm get-command-invocation \
    --region "$region" \
    --command-id "$cmd_id" \
    --instance-id "$instance_id" \
    --query 'StandardOutputContent' --output text
}

# tf_out reads a Terraform output from the active root module. Must
# be called from inside the root module directory (run.sh cd's there
# before invoking assertions).
tf_out() { terraform output -raw "$1"; }

log()  { printf '\033[36m▶\033[0m %s\n' "$*" >&2; }
ok()   { printf '\033[32m✓\033[0m %s\n' "$*" >&2; }
fail() { printf '\033[31m✗\033[0m %s\n' "$*" >&2; }
