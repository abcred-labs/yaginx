// pages/Home/index.tsx
import React from 'react';
import { Navigate, Outlet, Route, Routes, useParams } from 'react-router-dom';
// import TenantList from './TenantList';
// import TenantEdit from './TenantEdit';
// import TenantCreate from './TenantCreate';
// import TenantView from './TenantView';

const WebDomainIndex: React.FC = () => {
  return (
    <>
      <Routes>
        <Route index element={<Navigate to="/resource/hosts" replace />} />
        <Route path="catalog" element={<Navigate to="/resource/hosts" replace />} />
        <Route path="view/:host" element={<LegacyDomainViewRedirect />} />
        <Route path="list" element={<Navigate to="/resource/hosts" replace />} />
        {/* <Route path="list" element={<WebsiteList />} /> */}
        {/* <Route path="create" element={<TenantCreate />} />

        <Route path="detail/:id" element={<TenantView />} /> */}
      </Routes>
      <Outlet />
    </>
  )
}

const LegacyDomainViewRedirect: React.FC = () => {
  const { host = '' } = useParams();
  return <Navigate to={`/resource/hosts/view/${encodeURIComponent(decodeURIComponent(host))}`} replace />;
};

export default WebDomainIndex;
