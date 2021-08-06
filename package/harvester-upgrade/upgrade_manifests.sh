#!/bin/bash -ex

HOST_DIR="${HOST_DIR:-/host}"
UPGRADE_REPO_URL=http://upgrade-repo.harvester-system
UPGRADE_REPO_RELEASE_FILE="$UPGRADE_REPO_URL/harvester-iso/harvester-release.yaml"
UPGRADE_TMP_DIR="/tmp/upgrade"

wait_repo()
{
  until curl -sfL $UPGRADE_REPO_RELEASE_FILE
  do
    echo "Wait for upgrade repo ready..."
    sleep 5
  done
}

get_running_rancher_version()
{
  kubectl get settings.management.cattle.io server-version -o yaml | yq -e e '.value' -
}

upgrade_rancher()
{
  mkdir -p $UPGRADE_TMP_DIR/images
  mkdir -p $UPGRADE_TMP_DIR/rancher

  # Download rancher system agent install image from upgrade repo
  curl -sfL $UPGRADE_REPO_URL/harvester-iso/bundle/rancherd/images/rancherd-bootstrap-images.txt -o $UPGRADE_TMP_DIR/images/rancherd-bootstrap-images.txt
  curl -sfL $UPGRADE_REPO_URL/harvester-iso/bundle/rancherd/images/rancherd-bootstrap-images.tar.zst -o $UPGRADE_TMP_DIR/images/rancherd-bootstrap-images.tar.zst

  RANCHER_NEW_VERSION=$(sed -n 's/docker.io\/rancher\/system-agent-installer-rancher:\(.*\)/\1/p' $UPGRADE_TMP_DIR/images/rancherd-bootstrap-images.txt)
  if [ -z "$RANCHER_NEW_VERSION" ]; then
    echo "[ERROR] Fail to get Rancher version from upgrade repo."
    exit 0
  fi

  # Extract the Rancher chart and helm binary
  wharfie --images-dir $UPGRADE_TMP_DIR/images rancher/system-agent-installer-rancher:$RANCHER_NEW_VERSION $UPGRADE_TMP_DIR/rancher

  cd $UPGRADE_TMP_DIR/rancher

  ./helm get values rancher -n cattle-system -o yaml > values.yaml
  echo "Rancher values:"
  cat values.yaml

  RANCHER_CURRENT_VERSION=$(yq -e e '.rancherImageTag' values.yaml)
  if [ -z "$RANCHER_CURRENT_VERSION" ]; then
    echo "[ERROR] Fail to get current Rancher version."
    exit 0
  fi

  if [ "$RANCHER_CURRENT_VERSION" = "$RANCHER_NEW_VERSION" ]; then
    echo "Skip update Rancher. The version is already $RANCHER_CURRENT_VERSION"
    return
  fi 

  RANCHER_NEW_VERSION=$RANCHER_NEW_VERSION yq -e e '.rancherImageTag = strenv(RANCHER_NEW_VERSION)' values.yaml -i
  ./helm upgrade rancher *.tgz --namespace cattle-system -f values.yaml

  # Wait until new version ready

  until [ "$(get_running_rancher_version)" = "$RANCHER_NEW_VERSION" ]
  do
    echo "Wait for Rancher to be upgraded."
    sleep 5
  done
}

upgrade_harvester()
{
  # TODO: to ManagedChart
  mkdir -p $UPGRADE_TMP_DIR/harvester
  cd $UPGRADE_TMP_DIR/harvester

  # TODO: check need for upgrade
  curl -sfL $UPGRADE_REPO_URL/harvester-iso/bundle/harvester/static/charts/harvester-0.0.0-dev.tgz -o $UPGRADE_TMP_DIR/harvester/chart.tgz
  $UPGRADE_TMP_DIR/rancher/helm get values harvester -n harvester-system -o yaml > values.yaml
  $UPGRADE_TMP_DIR/rancher/helm upgrade harvester *.tgz  -n harvester-system -f values.yaml

  # TODO: Is there a way to check Harvester is upgraded??
}

# trap "sleep 30m" EXIT

wait_repo
upgrade_rancher
#upgrade_rke2
upgrade_harvester
