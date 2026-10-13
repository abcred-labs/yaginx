import React, { Suspense } from "react";
import { Navigate, Outlet, Route, Routes, useLocation } from "react-router-dom";
import RequestHistoryList from "./RequestHistory/List";

const MonitorIndex: React.FC = () => {
  return (
    <>
      <Routes>
        <Route index element={<Navigate to="/monitor/traffic/request-trend" replace />} />
        <Route path="websites" element={<Navigate to="/monitor/traffic/request-trend" replace />} />
        <Route path="hostTraffic/*" element={<LegacyHostTrafficRedirect />} />
        <Route path="requestHistory/list" element={<Suspense><RequestHistoryList /></Suspense>} />
        <Route path="ipBlacklist/list" element={<Navigate to="/access-control/ip-blacklist" replace />} />
        <Route path="database-performance" element={<Navigate to="/systems/database" replace />} />
      </Routes>
      <Outlet />
    </>
  );
};

const LegacyHostTrafficRedirect: React.FC = () => {
  const location = useLocation();
  const suffix = location.pathname.split('/hostTraffic/')[1] || 'request-trend';
  return <Navigate to={`/monitor/traffic/${suffix}`} replace />;
};

export default MonitorIndex;
