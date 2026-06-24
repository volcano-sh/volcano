FROM mxnet/python:1.9.1_cpu_py3@sha256:aad3f8fa3658601c7d7ddc33fadc8ec92f5aa9edd47673b8d619a13e4da8e2ba

RUN apt-get update \
    && apt-get install -y git

RUN git clone --recursive https://github.com/apache/incubator-mxnet

ENTRYPOINT ["python3", "/mxnet/incubator-mxnet/example/image-classification/train_mnist.py"]
