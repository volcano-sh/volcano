FROM ubuntu:16.04
MAINTAINER volcano <maintainer@volcano.sh>
RUN  apt-get update --fix-missing \
     && apt-get install -y libopenmpi-dev openmpi-bin \
     && apt-get install -y git \
     && apt-get install -y build-essential \
     && apt-get install -y ssh \
     && apt-get clean
RUN git clone https://github.com/wesleykendall/mpitutorial \
    && cd mpitutorial/tutorials/mpi-hello-world/code \
    && make \
    && cp mpitutorial/tutorials/mpi-hello-world/code/mpi_hello_world /home/ \
    && apt-get remove git \
    && apt-get remove build-essential \
CMD /etc/init.d/ssh start