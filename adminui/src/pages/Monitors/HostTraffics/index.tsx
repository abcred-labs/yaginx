import NoFound from "@/layouts/NoFond";
import React from "react";
import { Navigate, Outlet, Route, Routes } from "react-router-dom";
import { HostTrafficWorkspace } from "./dashboard";
import HostRanking from "./HostRanking";
import RequestTrend from "./RequestTrend";

const HostTrafficIndex: React.FC = () => {
  return (
    <HostTrafficWorkspace>
      <Routes>
        <Route index element={<Navigate to="request-trend" replace />} />
        <Route path="request-trend" element={<RequestTrend />} />
        <Route path="host-ranking" element={<HostRanking />} />
        <Route path="request-contribution" element={<Navigate to="../host-ranking" replace />} />
        <Route path="bandwidth" element={<Navigate to="../host-ranking" replace />} />
        <Route path="list" element={<Navigate to="../host-ranking" replace />} />
        <Route path="*" element={<NoFound />} />
      </Routes>
      <Outlet />
    </HostTrafficWorkspace>
  );
};

export default HostTrafficIndex;
