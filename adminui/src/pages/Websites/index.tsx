// pages/Home/index.tsx
import React from 'react';
import { Navigate, Outlet, Route, Routes, useParams } from 'react-router-dom';
import List from './List';
import { Create } from './Create';
import { Edit } from './Edit';
import WebsiteAnalytics from '@/pages/ResourceViews/WebsiteAnalytics';

const WebsiteDetailRedirect: React.FC = () => {
  const { id = '' } = useParams();
  return <Navigate to={`/website/${encodeURIComponent(id)}/analytics`} replace />;
};

const WebsiteIndex: React.FC = () => {
  return (
    <>
      <Routes>
        <Route index element={<Navigate to="list" />} />
        <Route path="list" element={<List />} />
        <Route path="create" element={<Create />} />
        <Route path="view/:id" element={<WebsiteDetailRedirect />} />
        <Route path=":id/analytics" element={<WebsiteAnalytics />} />
        <Route path=":id" element={<Edit />} />
        <Route path=":id/hosts" element={<Edit section="hosts" />} />
        <Route path=":id/upstreams" element={<Navigate to="../routes" replace />} />
        <Route path=":id/routes" element={<Edit section="routes" />} />
        <Route path=":id/transforms" element={<Edit section="transforms" />} />
        <Route path=":id/responses" element={<Edit section="responses" />} />
        <Route path=":id/access" element={<Edit section="access" />} />
        <Route path=":id/history" element={<Edit section="history" />} />
      </Routes>
      <Outlet />
    </>
  )
}

export default WebsiteIndex;
