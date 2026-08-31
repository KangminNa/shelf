FROM nginx:alpine

# 프록시가 컨테이너 이름으로 직접 붙는다 — 호스트 포트는 쓰지 않는다.
ARG PORT=4023
ENV PORT=${PORT}

COPY index.html /usr/share/nginx/html/index.html
RUN sed -i "s/listen  *80;/listen ${PORT};/" /etc/nginx/conf.d/default.conf \
 && sed -i "s/listen  *\[::\]:80;/listen [::]:${PORT};/" /etc/nginx/conf.d/default.conf

EXPOSE 4023
