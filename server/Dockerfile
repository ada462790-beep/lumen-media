# Lumen als Container.
#
# Gebaut wird vom Wurzelverzeichnis aus, nicht aus server/ — die Lizenz
# gehört mit ins Abbild, und die liegt eine Ebene höher:
#
#   docker build -f server/Dockerfile -t lumen .
#   docker run -d --name lumen -p 8097:8097 \
#     -v /pfad/zu/lumen-daten:/data -v /pfad/zu/filmen:/pfad/zu/filmen:rw lumen
#
# Zwei Stufen: bauen in einem vollen Go-Abbild, ausliefern in einem kleinen.
# Was am Ende übrig bleibt, ist die Binärdatei plus FFmpeg.

FROM golang:1.24-alpine AS bau

RUN apk add --no-cache build-base
WORKDIR /quelle

# Erst die Abhängigkeiten, dann der Rest: so muss Docker beim Ändern einer
# Zeile Code nicht alles neu holen.
COPY server/go.mod server/go.sum ./
COPY server/vendor ./vendor
COPY server/ .

ARG VERSION=0.1.0
RUN CGO_ENABLED=1 go build -mod=vendor -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /lumen ./cmd/lumen

# ---------------------------------------------------------------- ausliefern

FROM alpine:3.20

# FFmpeg ist das Wichtigste, was Lumen zur Laufzeit braucht. tzdata, damit
# Zeiten in der eigenen Zeitzone stimmen; ca-certificates für die
# Metadaten-Quellen; libheif-tools für HEIC-Fotos vom iPhone. Die
# VAAPI-Treiber (Intel/AMD) machen die Hardware-Beschleunigung möglich,
# sobald /dev/dri durchgereicht wird — ohne Gerät schaden sie nicht. Den
# Intel-Treiber gibt es nur für x86_64.
RUN apk add --no-cache ffmpeg tzdata ca-certificates su-exec libheif-tools \
      libva mesa-va-gallium \
 && if [ "$(apk --print-arch)" = "x86_64" ]; then \
      apk add --no-cache intel-media-driver libva-intel-driver onevpl-intel-gpu || \
      apk add --no-cache intel-media-driver; fi \
 && addgroup -g 1000 lumen \
 && adduser -D -u 1000 -G lumen lumen \
 && mkdir -p /data && chown lumen:lumen /data

COPY --from=bau /lumen /usr/local/bin/lumen
COPY server/docker-start.sh /usr/local/bin/lumen-start

# Die AGPL verlangt, dass der Lizenztext jede Kopie begleitet — ein Abbild
# ist eine Kopie. Darum liegt er hier mit drin und nicht nur im Lesemich.
COPY LICENSE /usr/share/licenses/lumen/LICENSE

LABEL org.opencontainers.image.title="Lumen" \
      org.opencontainers.image.description="Eigener Streaming-Server fürs Heimnetz" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.source="https://github.com/ada462790-beep/lumen-media"

# /data hält Datenbank, Bilder und Zwischenspeicher.
# Die Filme hängt man mit :rw ein: Lumen führt Filme in Teilen (a/b/c,
# CD1/CD2) auf Wunsch zu einer Datei zusammen. Siehe README.md.
VOLUME ["/data"]
ENV LUMEN_DATA=/data LUMEN_ADDR=:8097
EXPOSE 8097

# Kein USER hier: der Container startet als root, damit lumen-start den
# Datenordner passend machen kann, und gibt die Rechte dann ab. Lumen
# selbst läuft nie als root. Siehe server/docker-start.sh.

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD wget -qO- http://127.0.0.1:8097/api/v1/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/lumen-start"]
