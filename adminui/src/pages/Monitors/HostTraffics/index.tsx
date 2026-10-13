import NoFound from "@/layouts/NoFond";
import React from "react";
import { Navigate, Outlet, Route, Routes } from "react-router-dom";
import { HostTrafficWorkspace } from "./dashboard";
import BandwidthAnalysis from "./BandwidthAnalysis";
import List from "./List";
import RequestContribution from "./RequestContribution";
import RequestTrend from "./RequestTrend";

const HostTrafficIndex: React.FC = () => {
  return (
    <HostTrafficWorkspace>
      <Routes>
        <Route index element={<Navigate to="request-trend" replace />} />
        <Route path="request-trend" element={<RequestTrend />} />
        <Route path="request-contribution" element={<RequestContribution />} />
        <Route path="bandwidth" element={<BandwidthAnalysis />} />
        <Route path="list" element={<List />} />
        <Route path="*" element={<NoFound />} />
      </Routes>
      <Outlet />
    </HostTrafficWorkspace>
  );
};

export default HostTrafficIndex;
