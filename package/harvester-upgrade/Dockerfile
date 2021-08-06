FROM registry.suse.com/suse/sle15:15.3

ARG ARCH=amd64
ENV ARCH=${ARCH}
RUN zypper rm -y container-suseconnect
RUN zypper ar --priority=200 http://download.opensuse.org/distribution/leap/15.3/repo/oss repo-oss
RUN zypper --no-gpg-checks ref
RUN zypper in -y curl e2fsprogs rsync awk zstd jq

# TODO(kiefer): can we extract the kubectl from RKE2 image? Or use the one on host
ENV KUBECTL_VERSION v1.20.4
RUN curl -sfL https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl > /usr/bin/kubectl && \
    chmod +x /usr/bin/kubectl

RUN curl -sfL https://github.com/kubevirt/kubevirt/releases/download/v0.45.0/virtctl-v0.45.0-linux-${ARCH} -o /usr/bin/virtctl && chmod +x /usr/bin/virtctl

RUN curl -sfL https://github.com/mikefarah/yq/releases/download/v4.14.1/yq_linux_${ARCH} -o /usr/bin/yq && chmod +x /usr/bin/yq
RUN curl -sfL https://github.com/rancher/wharfie/releases/latest/download/wharfie-amd64  -o /usr/bin/wharfie && chmod +x /usr/bin/wharfie

COPY upgrade_node.sh /usr/local/bin/
COPY upgrade_manifests.sh /usr/local/bin/
COPY cos-upgrade /usr/local/bin/
