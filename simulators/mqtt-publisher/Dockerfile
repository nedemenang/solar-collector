FROM node:20-bookworm-slim
WORKDIR /app
COPY package.json ./
RUN npm install --omit=dev
COPY publish.js state.js ./
CMD ["node", "publish.js"]
