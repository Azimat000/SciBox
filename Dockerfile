# Образ сервера ИОН (срез 28, T-27): один образ для `serve`, `worker`, `migrate`, `journals`.
# Сборка многоступенчатая: сайт (Node) и сервер (Go) собираются отдельно, в итоговый образ попадают только готовый
# бинарник, собранный сайт и файл продукта. В итоговом образе нет оболочки, пакетного менеджера и curl, процесс
# работает не от root, файловая система только для чтения (compose: read_only).
#
# Сборка из корня репозитория:  docker build -t scibox:dev .
# Версия в `scibox version`:    docker build --build-arg VERSION=$(git describe --tags --always) -t scibox:1.0 .
#
# Базовые образы заданы переменными: если Docker Hub или gcr.io недоступны, подставьте адрес зеркала
# (docs/DEPLOY.md, «Если Docker Hub недоступен»):  --build-arg GO_IMAGE=mirror.example/library/golang:1.26.9-trixie

ARG NODE_IMAGE=node:24-trixie-slim
ARG GO_IMAGE=golang:1.26.9-trixie
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian13:nonroot

# --- сайт ---
FROM ${NODE_IMAGE} AS web
WORKDIR /src
# Сначала только описание зависимостей: слой с node_modules не пересобирается при правке исходников.
COPY web/package.json web/package-lock.json web/
RUN --mount=type=cache,target=/root/.npm cd web && npm ci --no-audit --no-fund
# config/product.json читает и сайт (название продукта, D-029).
COPY config config
# Примеры ответов чата (contracts/chat): на них стоят типы и тесты сайта, `tsc -b` их проверяет (срез 34).
COPY contracts contracts
COPY web web
RUN cd web && npm run build

# --- сервер ---
FROM ${GO_IMAGE} AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server ./
ARG VERSION=dev
# CGO выключен: бинарник статический и работает в образе без библиотек. -trimpath убирает пути сборки.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOFLAGS=-mod=readonly go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X scibox/server/internal/cli.Version=${VERSION}" -o /out/scibox ./cmd/api

# --- итоговый образ ---
FROM ${RUNTIME_IMAGE}
ARG VERSION=dev
LABEL org.opencontainers.image.title="scibox" \
      org.opencontainers.image.description="Сервер ИОН: поиск работы и вакансий в научной сфере России" \
      org.opencontainers.image.version="${VERSION}"
WORKDIR /app
COPY --from=server /out/scibox /app/scibox
COPY --from=web /src/web/dist /app/web
COPY config/product.json /app/config/product.json
# Образ боевой: SCIBOX_ENV=production не даёт запуститься с настройками для разработки (T-08). Слушать нужно все сети
# контейнера: наружу порт отдаёт compose (и не публикует).
ENV SCIBOX_ENV=production \
    SCIBOX_HTTP_ADDR=0.0.0.0:8080 \
    SCIBOX_WEB_DIR=/app/web \
    SCIBOX_PRODUCT_CONFIG=/app/config/product.json
EXPOSE 8080 9100
USER 65532:65532
# Готов ли процесс (база и миграции): /readyz внутреннего порта; у воркера в compose своя проверка.
HEALTHCHECK --interval=10s --timeout=4s --start-period=20s --retries=3 CMD ["/app/scibox", "healthcheck"]
ENTRYPOINT ["/app/scibox"]
CMD ["serve"]
