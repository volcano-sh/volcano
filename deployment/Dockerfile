From ubuntu:18.04

RUN apt-get update && apt-get install -y supervisor

ADD kar-scheduler /usr/local/bin
ADD kar-controllers /usr/local/bin
ADD karcli /usr/local/bin

ADD supervisord.conf /etc

CMD ["/usr/bin/supervisord", "-n"]
