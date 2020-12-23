FROM scratch
ADD galene LICENSE /
ADD /static /static
ADD /defaults/ /
CMD ["/galene"]
