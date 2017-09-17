FROM debian:jessie

WORKDIR /root

RUN apt update

##################################################
# locale settings
#ENV LC_ALL=zh_CN.UTF-8
RUN apt install -y locales
RUN sed -i -e 's/# zh_CN.UTF-8 UTF-8/zh_CN.UTF-8 UTF-8/' /etc/locale.gen && \
    echo 'LANG="zh_CN.UTF-8"'>/etc/default/locale && \
    dpkg-reconfigure --frontend=noninteractive locales && \
    update-locale LANG=zh_CN.UTF-8 UTF-8
RUN echo "Asia/Shanghai" > /etc/timezone
RUN dpkg-reconfigure -f noninteractive tzdata
ENV LC_ALL=zh_CN.UTF-8

##################################################
# install basic tools
RUN apt install -y curl wget && apt install -y git

##################################################
# install sshd
RUN apt install -y openssh-server && systemctl enable ssh

##################################################
# install gut dependencies
RUN apt install -y inotify-tools autoconf build-essential zlib1g-dev gettext &&
    apt install -y libssl-dev &&
    apt install -y net-tools

##################################################
# install golang
RUN wget https://storage.googleapis.com/golang/go1.9.linux-amd64.tar.gz &&
        tar -C /usr/local -xzf go1.9.linux-amd64.tar.gz &&
        rm ~/go1.9.linux-amd64.tar.gz
RUN echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.profile
ENV PATH=$PATH:/usr/local/go/bin

##################################################
# build gut-sync
# RUN bash -c "source ~/.profile; go get -v github.com/lovepocky/gut"
RUN go get -v github.com/lovepocky/gut
RUN ln -s ~/go/bin/gut /usr/local/bin/gut

##################################################
# build gut from git
RUN gut build

##################################################
# change ssh password
RUN echo "root:root" | chpasswd
RUN sed -i 's/PermitRootLogin without-password/PermitRootLogin yes/' /etc/ssh/sshd_config

##################################################

EXPOSE 22

CMD /sbin/init