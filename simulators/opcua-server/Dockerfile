FROM node:20-bookworm-slim
WORKDIR /app
COPY package.json ./
RUN npm install --omit=dev
COPY server.js state.js ./
EXPOSE 4840
CMD ["node", "server.js"]
