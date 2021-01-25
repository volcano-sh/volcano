FROM ubuntu:16.04
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
# Disable OpenSSH checking file modes and ownership of the user's files and home directory before accepting login
RUN cat /etc/ssh/sshd_config | grep -v StrictModes > /etc/ssh/sshd_config.new && \
    echo "StrictModes no" >> /etc/ssh/sshd_config.new && \
    mv /etc/ssh/sshd_config.new /etc/ssh/sshd_config
CMD mkdir -p /var/run/sshd; /usr/sbin/sshd;