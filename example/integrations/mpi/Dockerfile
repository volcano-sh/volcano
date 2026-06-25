FROM ubuntu:22.04@sha256:4f838adc7181d9039ac795a7d0aba05a9bd9ecd480d294483169c5def983b64d
MAINTAINER volcano <maintainer@volcano.sh>
RUN  apt-get update --fix-missing \
     && apt-get install -y libopenmpi-dev openmpi-bin \
     && apt-get install -y git \
     && apt-get install -y build-essential \
     && apt-get install -y ssh \
     && apt-get clean \
     && rm -rf /var/lib/apt/lists/*
RUN git clone https://github.com/wesleykendall/mpitutorial \
    && cd mpitutorial/tutorials/mpi-hello-world/code \
    && make \
    && cp mpi_hello_world /home/ \
    && apt-get autoremove -y git \
    && apt-get autoremove -y build-essential \
    && rm -rf "/mpitutorial"
CMD mkdir -p /var/run/sshd; /usr/sbin/sshd;
