FROM circleci/golang:1.11

COPY --from=gcr.io/google-containers/hyperkube:v1.13.1 /hyperkube /bin/kubectl

ENV KUSTOMIZE_VERSION 3.5.4
RUN curl -O -L https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2Fv${KUSTOMIZE_VERSION}/kustomize_v${KUSTOMIZE_VERSION}_linux_amd64.tar.gz
RUN tar xf kustomize_v${KUSTOMIZE_VERSION}_linux_amd64.tar.gz
RUN sudo mv kustomize /usr/local/bin/

COPY requirements.txt .
COPY Makefile .

RUN make ci-setup
RUN sudo apt-get update && sudo apt-get install python3-pip python3-venv && make venv
RUN sudo pip3 install awscli

RUN curl -sSL https://sdk.cloud.google.com | bash
ENV PATH $PATH:/home/circleci/google-cloud-sdk/bin

RUN curl -Lo kops https://github.com/kubernetes/kops/releases/download/$(curl -s https://api.github.com/repos/kubernetes/kops/releases/latest | grep tag_name | cut -d '"' -f 4)/kops-linux-amd64 && chmod +x ./kops && sudo mv ./kops /usr/local/bin/
