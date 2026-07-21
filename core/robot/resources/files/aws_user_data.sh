#!/bin/bash

# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

# Script used by the robot-framework.yaml workflow in GitHub Actions.
#
# This script contains a list of commands to run on a newly launched AWS instance,
# which gets passed into the 'aws ec2 run-instances --user-data ...' command.
#
# This script runs as root, so the 'sudo' prefix is not necessary on commands.

# Example:
# Set perf_event_paranoid kernel parameter
# sysctl kernel.perf_event_paranoid=-1

exec > /var/log/userdata.log 2>&1
set -ex
trap 'echo $? >/var/log/userdata.status' EXIT

if [ -f /etc/os-release ]; then
  . /etc/os-release
else
  echo "Unable to detect OS. /etc/os-release not found."
  exit 1
fi
echo "Detected OS: $ID ($PRETTY_NAME)"

# Retry wrapper for apt-get to handle transient failures and lock contention
apt_get_with_retry() {
  local max_attempts=5
  local attempt=1
  local delay=10
  while [ "${attempt}" -le "${max_attempts}" ]; do
    if DEBIAN_FRONTEND=noninteractive apt-get "$@"; then
      return 0
    fi
    echo "apt-get $* failed on attempt ${attempt}/${max_attempts}. Retrying in ${delay}s..."
    attempt=$((attempt + 1))
    sleep "${delay}"
  done
  echo "apt-get $* failed after ${max_attempts} attempts."
  return 1
}

########### Ensure python3 and python3-venv are available ###########
# We need `python3-venv` to install Python wheel-based assets,
# such as recipe tools used for static instruction mix analysis.
# On Debian-based systems (like Ubuntu), `python3-venv` is a separate package.

if ! command -v python3 >/dev/null 2>&1; then
  echo "Error: python3 not found. This system is not supported."
  exit 2
fi

check_python3_venv() {
  tmpdir=$(mktemp -d)
  result=0
  if ! python3 -m venv "$tmpdir/testenv" >/dev/null 2>&1; then
    result=1
  fi
  rm -rf "$tmpdir"
  return $result
}

if check_python3_venv; then
  echo "python3-venv is already available"
else
  echo "python3-venv is not available. Installing..."

  case "$ID" in
    ubuntu)
      apt_get_with_retry update
      apt_get_with_retry install -y python3-venv
      ;;
    *)
      echo "No explicit python3-venv installation defined for OS: $ID"
      echo "Assuming it is bundled with the default Python installation"
      ;;
  esac

  if check_python3_venv; then
    echo "python3-venv is now available"
  else
    echo "Failed to install or enable python3-venv"
    exit 3
  fi
fi

########### Ensure binutils is installed ###########
# We need `objdump` (from binutils) to support static instruction mix analysis.

if command -v objdump >/dev/null 2>&1; then
  echo "binutils (objdump) is already installed"
else
  echo "binutils (objdump) is not installed. Installing..."

  case "$ID" in
    ubuntu)
      apt_get_with_retry update
      apt_get_with_retry install -y binutils
      ;;
    amzn)
      if command -v dnf >/dev/null 2>&1; then
        dnf install -y binutils
      else
        yum install -y binutils
      fi
      ;;
    rhel | centos)
      yum install -y binutils
      ;;
    sles)
      zypper refresh
      zypper install -y binutils
      ;;
    *)
      echo "Unsupported OS for automatic binutils installation: $ID"
      ;;
  esac

  if command -v objdump >/dev/null 2>&1; then
    echo "binutils (objdump) installed successfully."
  else
    echo "Failed to install binutils (objdump)."
    exit 4
  fi
fi

########### Ensure Java 17+ is installed ###########
# We need Java to test Java stack collection during Robot recipe runs.
detect_java_major_version() {
  if ! command -v java >/dev/null 2>&1; then
    echo ""
    return
  fi
  java -version 2>&1 | awk -F'[\".]' '/version/ {print $2}'
}

JAVA_MAJOR_VERSION=$(detect_java_major_version)

if [ -n "${JAVA_MAJOR_VERSION}" ] && [ "${JAVA_MAJOR_VERSION}" -ge 17 ] 2>/dev/null; then
  echo "Java ${JAVA_MAJOR_VERSION} already installed"
else
  echo "Java 17+ not detected. Installing..."

  case "$ID" in
    ubuntu)
      apt_get_with_retry update
      apt_get_with_retry install -y openjdk-17-jdk
      ;;
    amzn)
      if command -v dnf >/dev/null 2>&1; then
        dnf install -y java-17-amazon-corretto
      else
        yum install -y java-17-amazon-corretto
      fi
      ;;
    rhel | centos)
      yum install -y java-17-openjdk-devel
      ;;
    sles)
      zypper refresh
      zypper install -y java-17-openjdk-devel
      ;;
    *)
      echo "Unsupported OS for automatic Java 17 installation: $ID"
      exit 5
      ;;
  esac

  JAVA_MAJOR_VERSION=$(detect_java_major_version)

  if [ -n "${JAVA_MAJOR_VERSION}" ] && [ "${JAVA_MAJOR_VERSION}" -ge 17 ] 2>/dev/null; then
    echo "Java ${JAVA_MAJOR_VERSION} installed successfully."
  else
    echo "Failed to install Java 17."
    exit 6
  fi
fi

########### Install AWS SSM Agent for debugging ###########
# If installtion fails, we silently ignore errors and continue --- may be adjusted in the future.
if ! command -v amazon-ssm-agent >/dev/null 2>&1; then
  echo "Installing AWS SSM Agent..."

  case "$ID" in
    ubuntu)
      if command -v snap >/dev/null 2>&1; then
        snap install amazon-ssm-agent --classic || echo "SSM Agent install failed; continuing" >&2
      else
        echo "snap not available; skipping SSM Agent install" >&2
      fi
      ;;
    amzn)
      if command -v dnf >/dev/null 2>&1; then
        dnf install -y amazon-ssm-agent || echo "SSM Agent install failed; continuing" >&2
      else
        yum install -y amazon-ssm-agent || echo "SSM Agent install failed; continuing" >&2
      fi
      systemctl enable amazon-ssm-agent || echo "Failed to enable amazon-ssm-agent; continuing" >&2
      systemctl start amazon-ssm-agent || echo "Failed to start amazon-ssm-agent; continuing" >&2
      ;;
    rhel | centos)
      yum install -y sudo dnf install -y https://s3.amazonaws.com/ec2-downloads-windows/SSMAgent/latest/linux_arm64/amazon-ssm-agent.rpm || \
        echo "SSM Agent install failed; continuing" >&2
      systemctl enable amazon-ssm-agent || echo "Failed to enable amazon-ssm-agent; continuing" >&2
      systemctl start amazon-ssm-agent || echo "Failed to start amazon-ssm-agent; continuing" >&2
      ;;
    sles)
      zypper refresh
      zypper install -y wget https://s3.amazonaws.com/ec2-downloads-windows/SSMAgent/latest/linux_arm64/amazon-ssm-agent.rpm || \
        echo "SSM Agent install failed; continuing" >&2
      systemctl enable amazon-ssm-agent || echo "Failed to enable amazon-ssm-agent; continuing" >&2
      systemctl start amazon-ssm-agent || echo "Failed to start amazon-ssm-agent; continuing" >&2
      ;;
    *)
      echo "Unsupported OS for automatic SSM Agent installation: $ID"
      ;;
  esac

  if command -v amazon-ssm-agent.ssm-cli >/dev/null 2>&1; then
    echo "AWS SSM Agent installed successfully."
  else
    echo "Failed to install AWS SSM Agent; continuing" >&2
  fi
else
  echo "AWS SSM Agent is already installed."
fi

########### Ensure we extend disk size inside OS ###########
# The EBS volume is resized via AWS APIs, but we need to extend the filesystem
# inside the OS to make use of the additional space.
# More info: https://docs.aws.amazon.com/ebs/latest/userguide/recognize-expanded-volume-linux.html

need_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "Missing required command: $1" >&2; exit 8; }; }

if [[ $EUID -ne 0 ]]; then
  echo "You must run as root (sudo)." >&2
  exit 9
fi

need_cmd findmnt
need_cmd lsblk

ROOT_SRC="$(findmnt -n -o SOURCE /)"
ROOT_FSTYPE="$(findmnt -n -o FSTYPE /)"

echo "Root source:   $ROOT_SRC"
echo "Root fstype:   $ROOT_FSTYPE"

# Walk "up" device-mapper stack to reach an underlying partition (if any).
DEV="$ROOT_SRC"
for _ in {1..8}; do
  # If DEV is a mapper path, resolve to dm-X
  if [[ "$DEV" == /dev/mapper/* ]]; then
    DM_NAME="$(lsblk -n -o NAME "$DEV" | head -n1)"
    [[ -n "${DM_NAME:-}" ]] && DEV="/dev/$DM_NAME"
  fi

  PK="$(lsblk -n -o PKNAME "$DEV" 2>/dev/null | head -n1 || true)"
  if [[ -z "${PK:-}" ]]; then
    break
  fi
  DEV="/dev/$PK"
done

UNDERLYING="$DEV"
echo "Underlying device candidate: $UNDERLYING"

# If root is LVM, we want the PV partition beneath the dm device.
PV_PART=""
if [[ "$ROOT_SRC" == /dev/mapper/* || "$ROOT_SRC" == /dev/dm-* ]]; then
  # Find first partition in the chain under the dm device
  # Try: dm -> PKNAME -> ... until we reach something that looks like a partition.
  C="$ROOT_SRC"
  for _ in {1..12}; do
    # Resolve mapper to dm
    if [[ "$C" == /dev/mapper/* ]]; then
      DM_NAME="$(lsblk -n -o NAME "$C" | head -n1)"
      [[ -n "${DM_NAME:-}" ]] && C="/dev/$DM_NAME"
    fi
    PK="$(lsblk -n -o PKNAME "$C" 2>/dev/null | head -n1 || true)"
    [[ -z "${PK:-}" ]] && break
    C="/dev/$PK"
    # If it ends with a digit (xvda3) or p<digit> (nvme0n1p3), treat as partition.
    if [[ "$C" =~ [0-9]$ ]]; then
      PV_PART="$C"
      break
    fi
  done
fi

TARGET_PART=""
if [[ -n "$PV_PART" ]]; then
  TARGET_PART="$PV_PART"
  echo "Detected LVM; PV partition: $TARGET_PART"
else
  # Non-LVM case: root itself is the partition device.
  TARGET_PART="$ROOT_SRC"
  echo "Detected non-LVM; root partition: $TARGET_PART"
fi

# Derive base disk + partition number
BASE_DISK=""
PART_NUM=""

if [[ "$TARGET_PART" =~ ^/dev/nvme[0-9]+n[0-9]+p([0-9]+)$ ]]; then
  PART_NUM="${BASH_REMATCH[1]}"
  BASE_DISK="${TARGET_PART%p$PART_NUM}"
elif [[ "$TARGET_PART" =~ ^/dev/[a-z]+d[a-z]([0-9]+)$ ]]; then
  PART_NUM="${BASH_REMATCH[1]}"
  BASE_DISK="${TARGET_PART%$PART_NUM}"
elif [[ "$TARGET_PART" =~ ^/dev/xvd[a-z]([0-9]+)$ ]]; then
  PART_NUM="${BASH_REMATCH[1]}"
  BASE_DISK="${TARGET_PART%$PART_NUM}"
else
  echo "Could not parse base disk/partition from: $TARGET_PART" >&2
  echo "If your root is more complex (RAID, encrypted, etc.), handle manually." >&2
  exit 10
fi

if [[ "$BASE_DISK" == /dev/nvme* ]]; then
  echo "Instance type guess: Nitro (NVMe root disk)"
else
  echo "Instance type guess: Xen (non-NVMe root disk)"
fi

echo "Base disk:     $BASE_DISK"
echo "Partition num: $PART_NUM"

# Ensure growpart exists (AWS guidance commonly uses it)
if ! command -v growpart >/dev/null 2>&1; then
  echo "growpart not found. Install cloud-utils-growpart (distro package) then re-run." >&2
  exit 11
fi

echo "==> Growing partition..."
# Any errors in partition growth or filesystem resize are non-fatal; we just print messages and continue.
SKIP_FS_RESIZE=0
if ! growpart_output="$(growpart "$BASE_DISK" "$PART_NUM" 2>&1)"; then
  if echo "$growpart_output" | grep -q "NOCHANGE"; then
    echo "$growpart_output"
    # Partition already at maximum size; nothing to do. Continue to filesystem resize --- even if the partition can't grow,
    # the filesystem might still be smaller than the partition.
  else
    echo "$growpart_output" >&2
    echo "growpart failed; skipping filesystem resize." >&2
    # Skip filesystem resize if growpart fails. We can't trust that the partition table reflects the intended size.
    SKIP_FS_RESIZE=1
  fi
fi

if [[ "$SKIP_FS_RESIZE" -eq 0 ]]; then
  # Re-read partition table
  if command -v partprobe >/dev/null 2>&1; then
    partprobe "$BASE_DISK" || true
  fi

  # If LVM, resize PV and extend LV + filesystem
  if [[ -n "$PV_PART" ]]; then
    need_cmd pvresize
    need_cmd lvextend

    echo "==> Resizing PV..."
    pvresize "$PV_PART"

    echo "==> Extending LV that backs / (and filesystem)..."
    # -r grows filesystem too (xfs/ext4 supported)
    lvextend -l +100%FREE -r "$ROOT_SRC"

    echo "Done. Check:"
    df -h /
  fi

  # Non-LVM: grow filesystem
  echo "==> Growing filesystem..."
  case "$ROOT_FSTYPE" in
    xfs)
      need_cmd xfs_growfs
      xfs_growfs /
      ;;
    ext4|ext3|ext2)
      need_cmd resize2fs
      resize2fs "$TARGET_PART"
      ;;
    *)
      echo "Unsupported/unknown filesystem type '$ROOT_FSTYPE'. Extend manually." >&2
      exit 12
      ;;
  esac

  echo "Done. Check:"
  df -h /
else
  echo "Skipping filesystem resize due to growpart failure." >&2
fi
