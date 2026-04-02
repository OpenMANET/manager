// =============================================================================
// AppRouter.jsx — Route definitions
// =============================================================================

import { Routes, Route } from 'react-router-dom';
import Layout from './Layout.jsx';
import DashboardPage from './pages/Dashboard.jsx';
import CommsPage from './App.jsx';
import SettingsPage from './pages/Settings.jsx';
import GpsStatusPage from './pages/GpsStatus.jsx';
import BLOSPage from './pages/BLOS.jsx';
import LoginPage from './pages/LoginPage.jsx';
import ProtectedRoute from './components/ProtectedRoute.jsx';

export default function AppRouter() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<ProtectedRoute><Layout /></ProtectedRoute>}>
        <Route path="/" element={<DashboardPage />} />
        <Route path="/comms" element={<CommsPage />} />
        <Route path="/gps" element={<GpsStatusPage />} />
        <Route path="/blos" element={<BLOSPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
    </Routes>
  );
}
