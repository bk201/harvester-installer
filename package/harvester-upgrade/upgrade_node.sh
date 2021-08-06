#!/bin/bash -e

PROG=$0
usage()
{
    echo "Usage: $PROG [--prepare] [--debug]"
    exit 1
}

HOST_DIR="${HOST_DIR:-/host}"

is_upgraded()
{
  if diff /etc/buildtime $HOST_DIR/etc/buildtime >/dev/null; then
      return 0
  fi
  return 1
}

upgrade()
{
  if is_upgraded; then
    echo Skip upgrade because the system is already upgraded.
    kubectl taint node $SYSTEM_UPGRADE_NODE_NAME kubevirt.io/drain- || true
    return 0
  fi

  mount --rbind $HOST_DIR/dev /dev
  mount --rbind $HOST_DIR/run /run
  
  # Create a config to write version label on next boot.
  # So if the label appears on the node, we know it's already rebooted.
  cat > /host/oem/90-node-label.yaml << EOF
name: set node version label
stages:
  initramfs:
    - files:
      -  path: /etc/rancher/rke2/config.yaml.d/90-upgrade.yaml
         permissions: 384
         owner: 0
         group: 0
         content: |
           node-label+:
            - harvesterhci.io/node-version=${SYSTEM_UPGRADE_PLAN_LATEST_VERSION}
         encoding: ""
         ownerstring: ""
EOF
  bash -x cos-upgrade --directory /
  nsenter -i -m -t 1 -- shutdown -r +1
}

# prepare is run before node draining
prepare()
{
  if is_upgraded; then
    echo Skip prepare because the system is already upgraded.
    kubectl taint node $SYSTEM_UPGRADE_NODE_NAME kubevirt.io/drain- || true
    return 0
  fi

  # check if the previous upgrade node is already rebooted
  NAMESPACE="cattle-system"
  plan_labels=$(kubectl get plans.upgrade.cattle.io -n $NAMESPACE $SYSTEM_UPGRADE_PLAN_NAME -o jsonpath='{.metadata.labels}')
  upgrade=$(echo $plan_labels| jq -r '."harvesterhci.io/upgrade"')
  upgraded_nodes=$(kubectl get upgrades.harvesterhci.io -n harvester-system $upgrade -o jsonpath='{.status.nodeStatuses}' | jq -r 'to_entries | map(select(.value.state == "Succeeded")) | .[].key')
  for node in $upgraded_nodes; do
    node_version=$(kubectl get node $node -o jsonpath='{.metadata.labels}' | jq -r '."harvesterhci.io/node-version"')
    echo $node_version

    until [ $node_version = $SYSTEM_UPGRADE_PLAN_LATEST_VERSION ]
    do
       echo "wait for node $node to be upgraded..."
       sleep 5
       node_version=$(kubectl get node $node -o jsonpath='{.metadata.labels}' | jq -r '."harvesterhci.io/node-version"')
    done
    kubectl taint node $node kubevirt.io/drain- || true
  done

  echo "Running prepare scripts"
  kubectl taint node $SYSTEM_UPGRADE_NODE_NAME kubevirt.io/drain=draining:NoSchedule
  echo "Finished prepare scripts"
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

if [ "$HARVESTER_UPGRADE_PREPARE" = "true" ]; then
  prepare
else
  upgrade
fi
