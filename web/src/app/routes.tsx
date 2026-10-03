import type { RouteObject } from 'react-router'
import { AccountPage } from '../features/auth/AccountPage'
import { ConfirmEmailPage } from '../features/auth/ConfirmEmailPage'
import { ForgotPasswordPage } from '../features/auth/ForgotPasswordPage'
import { LoginPage } from '../features/auth/LoginPage'
import { PrivacyPage } from '../features/auth/PrivacyPage'
import { RegisterPage } from '../features/auth/RegisterPage'
import { ResetPasswordPage } from '../features/auth/ResetPasswordPage'
import { CatalogPage } from '../features/catalog/CatalogPage'
import { AcceptInvitationPage } from '../features/orgs/AcceptInvitationPage'
import { CreateOrganizationPage } from '../features/orgs/CreateOrganizationPage'
import { ManageDataPage } from '../features/orgs/ManageDataPage'
import { ManageLayout } from '../features/orgs/ManageLayout'
import { ManageMembersPage } from '../features/orgs/ManageMembersPage'
import { ManageUnitsPage } from '../features/orgs/ManageUnitsPage'
import { MyOrganizationsPage } from '../features/orgs/MyOrganizationsPage'
import { OrganizationPage } from '../features/orgs/OrganizationPage'
import { OrganizationsPage } from '../features/orgs/OrganizationsPage'
import { UnitEditPage } from '../features/orgs/UnitEditPage'
import { UnitPage } from '../features/orgs/UnitPage'
import { ProfileEditPage } from '../features/profile/ProfileEditPage'
import { ProfilePage } from '../features/profile/ProfilePage'
import { ScientistPage } from '../features/profile/ScientistPage'
import { ApplicationPage } from '../features/applications/ApplicationPage'
import { ApplyPage } from '../features/applications/ApplyPage'
import { CandidatePage } from '../features/applications/CandidatePage'
import { CandidatesPage } from '../features/applications/CandidatesPage'
import { MyApplicationsPage } from '../features/applications/MyApplicationsPage'
import { RecommendPage } from '../features/applications/RecommendPage'
import { NotificationsPage } from '../features/notifications/NotificationsPage'
import { DeadlinesPage } from '../features/matching/DeadlinesPage'
import { FavoritesPage } from '../features/matching/FavoritesPage'
import { MatchesPage } from '../features/matching/MatchesPage'
import { OpenSavedSearchPage, SavedSearchesPage } from '../features/matching/SavedSearchesPage'
import { MyOffersPage } from '../features/offers/MyOffersPage'
import { OfferPage } from '../features/offers/OfferPage'
import { SentOffersPage } from '../features/offers/SentOffersPage'
import { MyVacanciesPage } from '../features/vacancies/MyVacanciesPage'
import { VacancyEditPage } from '../features/vacancies/VacancyEditPage'
import { VacancyPage } from '../features/vacancies/VacancyPage'
import { NotFoundPage } from '../features/status/NotFoundPage'
import { SearchPage } from '../features/search/SearchPage'
import { StatusPage } from '../features/status/StatusPage'
import { StyleguidePage } from '../features/styleguide/StyleguidePage'
import { CrashPage } from './CrashPage'
import { Layout } from './Layout'

export const routes: RouteObject[] = [
  {
    element: <Layout />,
    errorElement: <CrashPage />,
    children: [
      { index: true, element: <SearchPage /> },
      { path: 'vacancies', element: <SearchPage /> },
      { path: 'status', element: <StatusPage /> },
      { path: 'login', element: <LoginPage /> },
      { path: 'register', element: <RegisterPage /> },
      { path: 'forgot-password', element: <ForgotPasswordPage /> },
      { path: 'reset-password', element: <ResetPasswordPage /> },
      { path: 'confirm-email', element: <ConfirmEmailPage /> },
      { path: 'account', element: <AccountPage /> },
      { path: 'privacy', element: <PrivacyPage /> },
      { path: 'organizations', element: <OrganizationsPage /> },
      { path: 'organizations/new', element: <CreateOrganizationPage /> },
      { path: 'organizations/:slug', element: <OrganizationPage /> },
      { path: 'organizations/:slug/units/:unitId', element: <UnitPage /> },
      { path: 'my-organization', element: <MyOrganizationsPage /> },
      {
        path: 'my-organization/:slug',
        element: <ManageLayout />,
        children: [
          { index: true, element: <ManageDataPage /> },
          { path: 'units', element: <ManageUnitsPage /> },
          { path: 'units/new', element: <UnitEditPage /> },
          { path: 'units/:unitId', element: <UnitEditPage /> },
          { path: 'members', element: <ManageMembersPage /> },
        ],
      },
      { path: 'my-vacancies', element: <MyVacanciesPage /> },
      { path: 'my-vacancies/new', element: <VacancyEditPage /> },
      { path: 'my-vacancies/:id/edit', element: <VacancyEditPage /> },
      { path: 'vacancies/:id', element: <VacancyPage /> },
      { path: 'vacancies/:id/apply', element: <ApplyPage /> },
      { path: 'applications', element: <MyApplicationsPage /> },
      { path: 'applications/:id', element: <ApplicationPage /> },
      { path: 'candidates', element: <CandidatesPage /> },
      { path: 'candidates/:id', element: <CandidatePage /> },
      { path: 'recommend', element: <RecommendPage /> },
      { path: 'notifications', element: <NotificationsPage /> },
      { path: 'profile', element: <ProfilePage /> },
      { path: 'profile/edit', element: <ProfileEditPage /> },
      { path: 'scientists', element: <CatalogPage /> },
      { path: 'scientists/:id', element: <ScientistPage /> },
      { path: 'offers', element: <MyOffersPage /> },
      { path: 'offers/:id', element: <OfferPage /> },
      { path: 'sent-offers', element: <SentOffersPage /> },
      { path: 'favorites', element: <FavoritesPage /> },
      { path: 'matches', element: <MatchesPage /> },
      { path: 'saved-searches', element: <SavedSearchesPage /> },
      { path: 'saved-searches/:id', element: <OpenSavedSearchPage /> },
      { path: 'deadlines', element: <DeadlinesPage /> },
      { path: 'invitations/accept', element: <AcceptInvitationPage /> },
      { path: 'styleguide', element: <StyleguidePage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]
