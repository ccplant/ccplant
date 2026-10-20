FROM oven/bun:1.3.5 AS build
WORKDIR /app
COPY package.json ./
RUN bun install --frozen-lockfile
COPY . .
RUN bun run build
FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
