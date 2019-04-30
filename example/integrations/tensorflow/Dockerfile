#NOTE: the build process would change during developing.
FROM python:2.7
MAINTAINER volcano <maintainer@volcano.sh>
RUN  apt-get update --fix-missing \
&& apt-get install -y git \
&& apt-get clean \
&& rm -rf /var/lib/apt/lists/*
RUN pip install tf-nightly-gpu \
&& git clone https://github.com/tensorflow/benchmarks.git /opt/tf-benchmarks
