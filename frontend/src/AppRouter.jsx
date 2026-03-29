// =============================================================================
// AppRouter.jsx — Route definitions
// =============================================================================

import { Routes, Route } from 'react-router-dom';
import Layout from './Layout.jsx';
import CommsPage from './App.jsx';
import SettingsPage from './pages/Settings.jsx';

export default function AppRouter() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/" element={<CommsPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
    </Routes>
  );
}
