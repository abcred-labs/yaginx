import dayjs from 'dayjs';
import { ConfigEnv, UserConfig } from 'vite'
import { PORT, VITE_BASE_PATH, VITE_DROP_CONSOLE } from './config/constant';
import path from 'path';
import { createProxy } from './config/vite/proxy';
import pkg from './package.json';
import react from '@vitejs/plugin-react-swc';
import tailwindcss from '@tailwindcss/vite';

const { dependencies, devDependencies, name, version } = pkg;
const __APP_INFO__ = {
  pkg: { dependencies, devDependencies, name, version },
  lastBuildTime: dayjs().format('YYYY-MM-DD HH:mm:ss'),
};

// https://vitejs.dev/config/
export default ({ command, mode }: ConfigEnv): UserConfig => {
  console.log({ command, mode });
  return {
    base: VITE_BASE_PATH,
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: [
        { find: /^~/, replacement: path.resolve(__dirname, './') },
        { find: '@', replacement: path.resolve(__dirname, 'src') },
        { find: '@c', replacement: path.resolve(__dirname, 'config') },
      ],
    },
    server: {
      host: true,
      port: PORT, // 开发环境启动的端口
      proxy: createProxy(),
    },
    build: {
      terserOptions: {
        compress: {
          keep_infinity: true,
          // Used to delete console in production environment
          drop_console: VITE_DROP_CONSOLE,
        },
      },
      minify: "terser"
    },
    define: {
      // 设置应用信息
      __APP_INFO__: JSON.stringify(__APP_INFO__),
    },
  };
};
