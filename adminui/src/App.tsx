// App.tsx
import React, { Suspense } from 'react';
import { BrowserRouter, HashRouter, useRoutes } from 'react-router-dom';
import { VITE_BASE_PATH } from '@c/constant';
import routes, { IRouteConfig } from "./router/config";
import Loading from './components/Loading';
import { RootAuthProvider } from './layouts/RootAuthProvider';
import NoFound from './layouts/NoFond';
import { AppearanceProvider } from './appearance';
import { ConfirmDialogHost, Toaster } from '@modra/react';

const App: React.FC = () => {
  // 主路由构建
  const buildRouteData: any = (routeList?: IRouteConfig[]) => {
    if (routeList == null || routeList.length <= 0)
      return [];
    var routes = routeList.filter((route, i) => route.element !== undefined).map((route, i) => ({ path: route.routePath, children: buildRouteData(route.children), element: <route.element /> }));
    routes.push({ path: '*', element: <NoFound />, children: [] });
    return routes;
  }
  const RouteElement = () => useRoutes(buildRouteData(routes));
  return (
    <AppearanceProvider>
      <Suspense fallback={<Loading />}>
        <HashRouter>
          <RootAuthProvider>
            <RouteElement />
          </RootAuthProvider>
        </HashRouter>
      </Suspense>
      <Toaster />
      <ConfirmDialogHost />
    </AppearanceProvider>
  );
};

export default App;
