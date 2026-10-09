#!/usr/bin/env bash
# Records the cloud network traces the loader study needs, on two fresh VMs
# of one instance type in one zone:
#
#   gap-*.csv     one reused connection, idle for 100 ms / 1 s before each
#                 transfer: does an idle connection come back slower?
#   flows1.csv    one connection for 60 s, then
#   flows4-*.csv  four connections at once for 60 s: is one flow capped?
#   burst.csv     one reused connection for 30 min: does the burst
#                 allowance run out, and how far does bandwidth drop?
#
# counter_out.log and counter_in.log hold the ENA allowance counters every
# 5 s (unix time, bw_out/bw_in_allowance_exceeded), as in the c7i runs.
#
# Run from the repo root on a machine with AWS credentials:
#
#   KEY=kvflow SG=sg-... SUBNET=subnet-... scripts/aws-traces.sh [g5.xlarge]
#
# KEY is an EC2 key pair whose private key is ~/.ssh/$KEY.pem. SG must allow
# SSH from here and all traffic between its members. The VMs shut down and
# delete themselves after 100 minutes even if this script dies, and are
# terminated when it exits.
set -euo pipefail

TYPE=${1:-g5.xlarge}
REGION=${REGION:-us-east-2}
: "${KEY:?set KEY}" "${SG:?set SG}" "${SUBNET:?set SUBNET}"
OUT=results/aws-$(echo "$TYPE" | tr . -)-traces
SSH=(ssh -i ~/.ssh/"$KEY".pem -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=30)
mkdir -p "$OUT"

ids=$(aws ec2 run-instances --region "$REGION" \
  --image-id resolve:ssm:/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64 \
  --instance-type "$TYPE" --count 2 --key-name "$KEY" \
  --security-group-ids "$SG" --subnet-id "$SUBNET" --associate-public-ip-address \
  --instance-initiated-shutdown-behavior terminate \
  --user-data '#!/bin/sh
shutdown -h +100' \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=project,Value=kvflow}]' \
  --query 'Instances[].InstanceId' --output text)
trap 'aws ec2 terminate-instances --region "$REGION" --instance-ids $ids >/dev/null' EXIT
echo "launched $ids"
aws ec2 wait instance-status-ok --region "$REGION" --instance-ids $ids

read -r A B <<<"$ids"
pub() { aws ec2 describe-instances --region "$REGION" --instance-ids "$1" --query 'Reservations[0].Instances[0].PublicIpAddress' --output text; }
A_PUB=$(pub "$A"); B_PUB=$(pub "$B")
A_PRIV=$(aws ec2 describe-instances --region "$REGION" --instance-ids "$A" --query 'Reservations[0].Instances[0].PrivateIpAddress' --output text)

GOOS=linux GOARCH=amd64 go build -o /tmp/kvxfer ./cmd/kvxfer
for h in "$A_PUB" "$B_PUB"; do scp -i ~/.ssh/"$KEY".pem -o StrictHostKeyChecking=accept-new /tmp/kvxfer ec2-user@"$h": ; done

for h in "$A_PUB" "$B_PUB"; do scp -i ~/.ssh/"$KEY".pem scripts/ena-counter.sh ec2-user@"$h": ; done
"${SSH[@]}" ec2-user@"$A_PUB" 'nohup ./kvxfer recv -addr :9000 >recv.log 2>&1 </dev/null &
  nohup ./ena-counter.sh in >counter_in.log 2>&1 </dev/null &'
"${SSH[@]}" ec2-user@"$B_PUB" 'nohup ./ena-counter.sh out >counter_out.log 2>&1 </dev/null &'

send() { "${SSH[@]}" ec2-user@"$B_PUB" "./kvxfer send -addr $A_PRIV:9000 -reuse $*"; }
send -gap 100ms -duration 3m >"$OUT/gap-100ms.csv"
send -gap 1s -duration 3m >"$OUT/gap-1s.csv"
send -duration 60s >"$OUT/flows1.csv"
"${SSH[@]}" ec2-user@"$B_PUB" "for i in 1 2 3 4; do ./kvxfer send -addr $A_PRIV:9000 -reuse -duration 60s >flows4-\$i.csv & done; wait"
for i in 1 2 3 4; do scp -i ~/.ssh/"$KEY".pem ec2-user@"$B_PUB":flows4-$i.csv "$OUT/"; done
date +%s.%N >"$OUT/burst_start"
send -duration 30m >"$OUT/burst.csv"

scp -i ~/.ssh/"$KEY".pem ec2-user@"$A_PUB":counter_in.log "$OUT/"
scp -i ~/.ssh/"$KEY".pem ec2-user@"$B_PUB":counter_out.log "$OUT/"
echo "done: $OUT"
