FROM alpine:3.12

RUN mkdir -p /home/image-clone-controller/
ADD image-clone-controller /home/image-clone-controller
RUN chmod +x /home/image-clone-controller/image-clone-controller

USER 2121:2121

ENTRYPOINT ["/home/image-clone-controller/image-clone-controller"]