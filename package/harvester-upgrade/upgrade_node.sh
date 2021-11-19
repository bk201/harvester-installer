#!/bin/bash -ex

PROG=$0
usage()
{
    echo "Usage: $PROG [--prepare] [--debug]"
    exit 1
}

HOST_DIR="${HOST_DIR:-/host}"
UPGRADE_REPO_URL=http://upgrade-repo.harvester-system
UPGRADE_REPO_RELEASE_FILE="$UPGRADE_REPO_URL/harvester-iso/harvester-release.yaml"
UPGRADE_REPO_SQUASHFS_IMAGE="$UPGRADE_REPO_URL/harvester-iso/rootfs.squashfs"
UPGRADE_REPO_BUNDLE_ROOT="$UPGRADE_REPO_URL/harvester-iso/bundle"
UPGRADE_REPO_BUNDLE_METADATA="$UPGRADE_REPO_URL/harvester-iso/bundle/metadata.yaml"
UPGRADE_TMP_DIR=$HOST_DIR/usr/local/upgrade_tmp

reboot_if_job_succeed()
{
  cat > $HOST_DIR/tmp/upgrade-reboot.sh << EOF
#!/bin/bash -ex
SYSTEM_UPGRADE_POD_NAME=$SYSTEM_UPGRADE_POD_NAME

EOF

  cat >> $HOST_DIR/tmp/upgrade-reboot.sh << 'EOF'
source /etc/bash.bashrc.local
pod_id=$(crictl pods --name $SYSTEM_UPGRADE_POD_NAME --namespace cattle-system -o json | jq -er '.items[0].id')

# get `upgrade` container ID
container_id=$(crictl ps --pod $pod_id --name upgrade -o json -a | jq -er '.containers[0].id')
container_state=$(crictl inspect $container_id | jq -er '.status.state')

if [ "$container_state" = "CONTAINER_EXITED" ]; then
  container_exit_code=$(crictl inspect $container_id | jq -r '.status.exitCode')

  if [ "$container_exit_code" = "0" ]; then
    sleep 10
    reboot
    exit 0
  fi
fi

exit 1
EOF

  chmod +x $HOST_DIR/tmp/upgrade-reboot.sh

  cat > $HOST_DIR/run/systemd/system/upgrade-reboot.service << 'EOF'
[Unit]
Description=Upgrade reboot

[Service]
Type=simple
ExecStart=/tmp/upgrade-reboot.sh
Restart=always
RestartSec=10
EOF

  chroot $HOST_DIR systemctl daemon-reload
  chroot $HOST_DIR systemctl start upgrade-reboot
}

remove_old_manifests()
{
  rm -f $HOST_DIR/var/lib/rancher/rke2/server/manifests/harvester.yaml
  rm -f $HOST_DIR/var/lib/rancher/rke2/server/manifests/monitoring-crd.yaml
  rm -f $HOST_DIR/var/lib/rancher/rke2/server/manifests/monitoring-dashboard.yaml
  rm -f $HOST_DIR/var/lib/rancher/rke2/server/manifests/monitoring.yaml
  rm -f $HOST_DIR/var/lib/rancher/rke2/server/static/charts/harvester-*.tgz
  rm -f $HOST_DIR/var/lib/rancher/rke2/server/static/charts/rancher-monitoring-*.tgz
}

preload_images()
{
  export CONTAINER_RUNTIME_ENDPOINT=unix:///$HOST_DIR/run/k3s/containerd/containerd.sock
  export CONTAINERD_ADDRESS=$HOST_DIR/run/k3s/containerd/containerd.sock

  CTR="$HOST_DIR/$(readlink $HOST_DIR/var/lib/rancher/rke2/bin)/ctr"
  if [ -z "$CTR" ];then
    echo "Fail to get host ctr binary."
    exit 1
  fi

  metadata=$(mktemp --suffix=.yaml)
  curl -sfL $UPGRADE_REPO_BUNDLE_METADATA -o $metadata

  tmp_image_archives=$(mktemp -d -p $UPGRADE_TMP_DIR)

  # Common container images. Load with containerd.
  yq -e -o=json e '.images.common' $metadata | jq -r '.[] | [.list, .archive] | @tsv' |
    while IFS=$'\t' read -r list archive; do
      archive_name=$(basename -s .tar.zst $archive)
      image_list_url="$UPGRADE_REPO_BUNDLE_ROOT/$list"
      archive_url="$UPGRADE_REPO_BUNDLE_ROOT/$archive"
      image_list_file="${tmp_image_archives}/$(basename $list)"
      archive_file="${tmp_image_archives}/${archive_name}.tar"

      # Check if images already exist
      curl -sfL $image_list_url | sort > $image_list_file
      missing=$($CTR -n k8s.io images ls -q | grep -v ^sha256 | sort | comm -23 $image_list_file -)
      if [ -z "$missing" ]; then
        echo "Images in $image_list_file already present in the system. Skip preloading."
        continue
      fi

      curl -sfL $archive_url | zstd -d -f --no-progress -o $archive_file
      $CTR -n k8s.io image import $archive_file
      rm -f $archive_file
    done

  rm -rf $tmp_image_archives

  # RKE2 images: save tarballs
  rke2_prefix="${REPO_RKE2_VERSION/+/-}-"
  yq -e -o=json e '.images.rke2' $metadata | jq -r '.[] | [.list, .archive] | @tsv' |
    while IFS=$'\t' read -r list archive; do
      image_list_url="$UPGRADE_REPO_BUNDLE_ROOT/$list"
      archive_url="$UPGRADE_REPO_BUNDLE_ROOT/$archive"
      image_list_file="$HOST_DIR/var/lib/rancher/rke2/agent/images/${rke2_prefix}$(basename $list)"
      archive_file="$HOST_DIR/var/lib/rancher/rke2/agent/images/${rke2_prefix}$(basename $archive)"

      if [ ! -e $image_list_file ]; then
        curl -sfL $image_list_url -o $image_list_file
      fi

      if [ ! -e $archive_file ]; then
        curl -sfL $archive_url -o $archive_file
      fi
    done

  # Rancherd images: save tarballs
  rancherd_prefix="${REPO_OS_VERSION}-"
  yq -e -o=json e '.images.rancherd' $metadata | jq -r '.[] | [.list, .archive] | @tsv' |
    while IFS=$'\t' read -r list archive; do
      image_list_url="$UPGRADE_REPO_BUNDLE_ROOT/$list"
      archive_url="$UPGRADE_REPO_BUNDLE_ROOT/$archive"
      image_list_file="$HOST_DIR/var/lib/rancher/agent/images/${rancherd_prefix}$(basename $list)"
      archive_file="$HOST_DIR/var/lib/rancher/agent/images/${rancherd_prefix}$(basename $archive)"

      if [ ! -e $image_list_file ]; then
        curl -sfL $image_list_url -o $image_list_file
      fi

      if [ ! -e $archive_file ]; then
        curl -sfL $archive_url -o $archive_file
      fi
    done
}

command_upgrade()
{
  kubectl taint node $SYSTEM_UPGRADE_NODE_NAME kubevirt.io/drain- || true

  # Not needed if we switch to ManagedChart
  remove_old_manifests

  if [ "$NEED_REBOOT" != "y" ]; then
    label_node $SYSTEM_UPGRADE_NODE_NAME
    exit 0
  fi

  mount --rbind $HOST_DIR/dev /dev
  mount --rbind $HOST_DIR/run /run

  tmp_rootfs_squashfs=$(mktemp -p $UPGRADE_TMP_DIR)
  tmp_rootfs_mount=$(mktemp -d) 
  curl -sfL $UPGRADE_REPO_SQUASHFS_IMAGE -o $tmp_rootfs_squashfs
  mount $tmp_rootfs_squashfs $tmp_rootfs_mount

  bash -x $HOST_DIR/usr/sbin/cos-upgrade --directory $tmp_rootfs_mount
  umount $tmp_rootfs_mount
  rm -rf $tmp_rootfs_squashfs

  umount -R /run
  label_node $SYSTEM_UPGRADE_NODE_NAME
  kubectl uncordon $SYSTEM_UPGRADE_NODE_NAME || true
  reboot_if_job_succeed
  # nsenter -i -m -t 1 -- reboot
}

unlabel_node()
{
  kubectl label node $1 harvesterhci.io/latest-upgrade-node-
}

label_node()
{
  local node=$1
  kubectl label node $node harvesterhci.io/latest-upgrade-node=true
  
  boot_id=$(get_boot_id $node)
  kubectl label node $node harvesterhci.io/upgrade-last-boot-id=$boot_id --overwrite
  kubectl label node $node harvesterhci.io/upgrade-new-os-version=$REPO_OS_VERSION --overwrite
}

get_boot_id()
{
  kubectl get node $1 -o yaml | yq -e e '.status.nodeInfo.bootID' -
}

get_os_version()
{
  kubectl get node $1 -o yaml | yq -e e '.status.nodeInfo.osImage' -
}

replica_check_state()
{
  local namespace=$1
  local name=$2
  local replica_json
  local desire_state
  local current_state

  replica_json=$(kubectl get replicas.longhorn.io -n $namespace $name -o json)
  desire_state=$(echo "$replica_json" | jq -er '.spec.desireState')
  current_state=$(echo "$replica_json" | jq -er '.status.currentState')

  if [ "$desire_state" = "$current_state" ]; then
    return 0
  fi

  echo "Replica ${namespace}/${name} desireState: $desire_state , currentState: $current_state"
  return 1
}

wait_replica()
{ 
  local namespace=$1
  local name=$2

  echo "Waiting for replica ${namespace}/${name}..."

  until replica_check_state $namespace $name; do
    sleep 5
  done
}

wait_replicas_on_node()
{
  longhorn_node=$1

  kubectl get -A replicas.longhorn.io --selector longhornnode=$longhorn_node -o json |
    jq -r '.items[].metadata | [.name, .namespace] | @tsv' |
    while IFS=$'\t' read -r name namespace; do
      if [ -z $name ]; then
        break
      fi
      wait_replica $namespace $name
    done
}

wait_node_ready()
{
  local node=$1
  local new_os_version

  new_os_version=$( kubectl get node $node -o yaml | yq -e e '.metadata.labels."harvesterhci.io/upgrade-new-os-version"' -)
  until [ "$(get_os_version $node)" == "Harvester $new_os_version" ]
  do
    echo "Waiting for node $node to reboot..."
    sleep 10
  done

  until kubectl get node $node -o json | jq -r '.status.conditions[] | select(.type == "Ready" and .status == "True")'
  do
    echo "Waiting for node $node ready..."
    sleep 10
  done

  until ! kubectl get node $node -o json | jq -er 'select(.spec.unschedulable == true)'
  do
    echo "Waiting for node $node to be schedulable..."
    sleep 10
  done
}

get_instance_managers()
{
  local node=$1
  kubectl get instancemanagers.longhorn.io -A -l longhorn.io/node=$node -o json | jq -r '.items | map(select(.status.currentState == "running")) | length'
}

wait_instance_managers_on_node ()
{
  local node=$1

  until [ "$(get_instance_managers $node)" = "2" ]
  do
    echo "Waiting for instance managers on node $node..."
    sleep 5
  done
}

wait_last_node()
{
  local nodes

  nodes=$(kubectl get nodes --selector harvesterhci.io/latest-upgrade-node=true -o jsonpath='{.items[*].metadata.name}')
  for node in $nodes; do

    if [ "$node" = "$SYSTEM_UPGRADE_NODE_NAME" ]; then
      echo "Warning: skip waiting for myself"
      continue
    fi

    echo "Waiting for node ${node}..."
    wait_node_ready $node
    wait_instance_managers_on_node $node

    # If we start instance manager and migrate VMs immediately, the volume attaching might be stuck on other nodes
    sleep 60

    wait_replicas_on_node $node
    unlabel_node $node
    echo "Node $node is ready"
  done
}

wait_repo()
{
  until curl -sfL $UPGRADE_REPO_RELEASE_FILE
  do
    echo "Wait for upgrade repo ready..."
    sleep 5
  done
}

detect_repo()
{
  release_file=$(mktemp --suffix=.yaml)
  curl -sfL $UPGRADE_REPO_RELEASE_FILE -o $release_file

  REPO_OS_VERSION=$(yq -e e '.os' $release_file)
  REPO_RKE2_VERSION=$(yq -e e '.kubernetes' $release_file)

  if [ -z "$REPO_OS_VERSION" ]; then
    echo "Fail to get OS version in the upgrade repo"
    exit 1
  fi

  HOST_OS_PRETTY_NAME=$(bash -c 'source /host/etc/os-release && echo $PRETTY_NAME')
  if [ -z "$HOST_OS_PRETTY_NAME" ]; then
    echo "Fail to get OS pretty name for current node"
    exit 1
  fi

  NEED_REBOOT="n"
  if [ "$HOST_OS_PRETTY_NAME" != "Harvester $REPO_OS_VERSION" ]; then
    NEED_REBOOT="y"
  fi
}

get_running_vm_count()
{
  local count

  count=$(kubectl get vmi -A -l kubevirt.io/nodeName=$SYSTEM_UPGRADE_NODE_NAME -ojson | jq '.items | length' || true)
  echo $count
}

wait_vms_migrated()
{
  vm_count="$(get_running_vm_count)"
  until [ "$vm_count" = "0" ]
  do
    echo "Waiting for VM live-migration...($vm_count left)"
    sleep 5
    vm_count="$(get_running_vm_count)"
  done
}

stop_vms()
{
  echo "Stop VMs..."
  kubectl get vmi -A -l kubevirt.io/nodeName=$SYSTEM_UPGRADE_NODE_NAME -o json |
  jq -r  '.items[].metadata | [.name, .namespace] | @tsv' |
  while IFS=$'\t' read -r name namespace; do
    if [ -z "$name" ]; then
      break
    fi
    echo "Stop ${namespace}/${name}"
    virtctl stop $name -n $namespace
  done
}

command_prepare()
{
  wait_last_node
  preload_images

  if [ "$NEED_REBOOT" = "y" ]; then
    # Live migrate VMs
    kubectl taint node $SYSTEM_UPGRADE_NODE_NAME --overwrite kubevirt.io/drain=draining:NoSchedule

    # Wait for VM migrated
    wait_vms_migrated

    # KubeVirt's pdb might cause drain fail
    wait_evacuation_pdb_gone

    # TODO: disable eviction
    # Drain this node
    kubectl drain $SYSTEM_UPGRADE_NODE_NAME --pod-selector "!upgrade.cattle.io/controller" --force --ignore-daemonsets --delete-local-data
  else
    echo "Nothing to do?"
  fi
}

wait_evacuation_pdb_gone()
{
  # TODO: fine-tune this to per-VM check
  until ! kubectl get pdb -o name -A | grep kubevirt-migration-pdb-kubevirt-evacuation-
  do
    echo "Waiting for evacuation PDB gone..."
    sleep 5
  done
}

while [ "$#" -gt 0 ]; do
    case $1 in
        --debug)
            set -x
            ;;
        --prepare)
            HARVESTER_UPGRADE_PREPARE=true
            ;;
        *)
            break
            ;;
    esac
    shift 1
done

# TODO: In single node or shutdown mode, repo will be shutdown
wait_repo
detect_repo

mkdir -p $UPGRADE_TMP_DIR

if [ "$HARVESTER_UPGRADE_PREPARE" = "true" ]; then
  command_prepare
else
  command_upgrade
fi
