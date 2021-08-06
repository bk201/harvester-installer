ARG HARVESTER_OS_IMAGE
# FROM rancher/harvester-os:c22323d-dirty 
FROM ${HARVESTER_OS_IMAGE}

ARG ARCH=amd64
ENV KUBECTL_VERSION v1.20.4

COPY upgrade_node.sh /usr/local/bin/upgrade_node.sh

# TODO(kiefer): can we extract the kubectl from RKE2 image? Or use the one on host
RUN curl -sLf https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl > /usr/bin/kubectl && \
    chmod +x /usr/bin/kubectl
ENTRYPOINT ["upgrade_node.sh"]
