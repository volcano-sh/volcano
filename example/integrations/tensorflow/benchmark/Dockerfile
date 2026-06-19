# NOTE: the build process would change during developing,
# the commit ID when first creating the image: 62c833f806db621943a6cf8195657b9d0fa67d93 (master)
# original image is: gcr.io/kubeflow/tf-benchmarks-cpu:v20171202-bdab599-dirty-284af3,
# the image needs an update to use the latest tf-benchmark logic
# ref => https://github.com/tensorflow/benchmarks/tree/master/scripts/tf_cnn_benchmarks.
FROM python:2.7@sha256:cfa62318c459b1fde9e0841c619906d15ada5910d625176e24bf692cf8a2601d
MAINTAINER volcano <volcano-sh@googlegroups.com>
RUN  apt-get update --fix-missing \
&& apt-get install -y git \
&& apt-get clean \
&& rm -rf /var/lib/apt/lists/*
RUN pip install tf-nightly-gpu==1.15.0.dev20190827 \
&& git clone https://github.com/tensorflow/benchmarks.git /opt/tf-benchmarks
