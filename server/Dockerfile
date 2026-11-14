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

FROM golang:1.25-alpine AS bau

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

FROM alpine:3.22

# FFmpeg ist das Wichtigste, was Lumen zur Laufzeit braucht. tzdata, damit
# Zeiten in der eigenen Zeitzone stimmen; ca-certificates für die
# Metadaten-Quellen. Die VAAPI-Treiber (Intel/AMD) machen die
# Hardware-Beschleunigung möglich, sobald /dev/dri durchgereicht wird —
# ohne Gerät schaden sie nicht. Den Intel-Treiber gibt es nur für x86_64.
RUN apk add --no-cache ffmpeg tzdata ca-certificates su-exec \
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
# Die Bibliotheken in der Binärdatei (MIT, BSD) verlangen ihren Lizenzhinweis
# in jeder Kopie; die Liste der Drittsoftware nennt sie samt Text. Dazu die
# genauen Alpine-Pakete dieses Abbilds: Für FFmpeg, BusyBox und die übrigen
# GPL-Pakete muss sich der passende Quelltext finden lassen — bei Alpine
# (aports und distfiles) unter genau diesen Fassungen.
COPY LICENSE-EXCEPTIONS THIRD_PARTY.md DRITTSOFTWARE.md /usr/share/licenses/lumen/
RUN { echo "Alpine Linux $(cat /etc/alpine-release)"; \
      echo "Quelltext: https://distfiles.alpinelinux.org/distfiles/ und https://gitlab.alpinelinux.org/alpine/aports"; \
      echo; apk info -v | sort; } > /usr/share/licenses/lumen/alpine-pakete.txt

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

# Der Port kommt aus LUMEN_ADDR (wer ihn ändert, soll keinen dauerhaft
# „ungesunden" Container bekommen). Beim ersten Start nach einem Update
# laufen Sicherung und Umbau der Datenbank — dafür die längere Anlaufzeit.
# Hört Lumen nur auf einer festen Adresse (LUMEN_ADDR=192.168.1.20:8097,
# etwa im Host-Netz), fragt die Prüfung dort; sonst 127.0.0.1.
HEALTHCHECK --interval=30s --timeout=5s --start-period=120s \
  CMD h="${LUMEN_ADDR%:*}"; case "$h" in ""|0.0.0.0|"[::]") h=127.0.0.1;; esac; \
      wget -qO- "http://$h:${LUMEN_ADDR##*:}/api/v1/health" >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/lumen-start"]
