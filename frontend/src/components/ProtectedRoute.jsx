// =============================================================================
// ProtectedRoute.jsx — Redirect to /login if the user is not authenticated
// =============================================================================

import { Navigate } from 'react-router-dom';
import { useAuth } from '../contexts/useAuth.js';

export default function ProtectedRoute({ children }) {
  const { isAuthenticated, loading } = useAuth();

  // Wait for the initial session check before deciding.
  if (loading) return null;

  if (!isAuthenticated) return <Navigate to="/login" replace />;

  return children;
}
