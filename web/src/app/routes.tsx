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
import { LandingPage } from '../features/landing/LandingPage'
import { SearchPage } from '../features/search/SearchPage'
import { StatusPage } from '../features/status/StatusPage'
import { StyleguidePage } from '../features/styleguide/StyleguidePage'
import { CrashPage } from './CrashPage'
import { Layout } from './Layout'
import { t } from '../i18n'

export const routes: RouteObject[] = [
  {
    element: <Layout />,
    errorElement: <CrashPage />,
    children: [
      { index: true, element: <LandingPage /> },
      { path: 'vacancies', handle: { title: t.pageTitles.vacancies }, element: <SearchPage /> },
      { path: 'status', handle: { title: t.pageTitles.status }, element: <StatusPage /> },
      { path: 'login', handle: { title: t.pageTitles.login }, element: <LoginPage /> },
      { path: 'register', handle: { title: t.pageTitles.register }, element: <RegisterPage /> },
      { path: 'forgot-password', handle: { title: t.pageTitles.forgotPassword }, element: <ForgotPasswordPage /> },
      { path: 'reset-password', handle: { title: t.pageTitles.resetPassword }, element: <ResetPasswordPage /> },
      { path: 'confirm-email', handle: { title: t.pageTitles.confirmEmail }, element: <ConfirmEmailPage /> },
      { path: 'account', handle: { title: t.pageTitles.account }, element: <AccountPage /> },
      { path: 'privacy', handle: { title: t.pageTitles.privacy }, element: <PrivacyPage /> },
      { path: 'organizations', handle: { title: t.pageTitles.organizations }, element: <OrganizationsPage /> },
      { path: 'organizations/new', handle: { title: t.pageTitles.organizationsNew }, element: <CreateOrganizationPage /> },
      { path: 'organizations/:slug', element: <OrganizationPage /> },
      { path: 'organizations/:slug/units/:unitId', element: <UnitPage /> },
      { path: 'my-organization', handle: { title: t.pageTitles.myOrganization }, element: <MyOrganizationsPage /> },
      {
        path: 'my-organization/:slug',
        handle: { title: t.pageTitles.manage },
        element: <ManageLayout />,
        children: [
          { index: true, element: <ManageDataPage /> },
          { path: 'units', element: <ManageUnitsPage /> },
          { path: 'units/new', element: <UnitEditPage /> },
          { path: 'units/:unitId', element: <UnitEditPage /> },
          { path: 'members', element: <ManageMembersPage /> },
        ],
      },
      { path: 'my-vacancies', handle: { title: t.pageTitles.myVacancies }, element: <MyVacanciesPage /> },
      { path: 'my-vacancies/new', handle: { title: t.pageTitles.myVacanciesNew }, element: <VacancyEditPage /> },
      { path: 'my-vacancies/:id/edit', handle: { title: t.pageTitles.myVacanciesIdEdit }, element: <VacancyEditPage /> },
      { path: 'vacancies/:id', element: <VacancyPage /> },
      { path: 'vacancies/:id/apply', handle: { title: t.pageTitles.vacanciesIdApply }, element: <ApplyPage /> },
      { path: 'applications', handle: { title: t.pageTitles.applications }, element: <MyApplicationsPage /> },
      { path: 'applications/:id', element: <ApplicationPage /> },
      { path: 'candidates', handle: { title: t.pageTitles.candidates }, element: <CandidatesPage /> },
      { path: 'candidates/:id', element: <CandidatePage /> },
      { path: 'recommend', handle: { title: t.pageTitles.recommend }, element: <RecommendPage /> },
      { path: 'notifications', handle: { title: t.pageTitles.notifications }, element: <NotificationsPage /> },
      { path: 'profile', handle: { title: t.pageTitles.profile }, element: <ProfilePage /> },
      { path: 'profile/edit', handle: { title: t.pageTitles.profileEdit }, element: <ProfileEditPage /> },
      { path: 'scientists', handle: { title: t.pageTitles.scientists }, element: <CatalogPage /> },
      { path: 'scientists/:id', element: <ScientistPage /> },
      { path: 'offers', handle: { title: t.pageTitles.offers }, element: <MyOffersPage /> },
      { path: 'offers/:id', element: <OfferPage /> },
      { path: 'sent-offers', handle: { title: t.pageTitles.sentOffers }, element: <SentOffersPage /> },
      { path: 'favorites', handle: { title: t.pageTitles.favorites }, element: <FavoritesPage /> },
      { path: 'matches', handle: { title: t.pageTitles.matches }, element: <MatchesPage /> },
      { path: 'saved-searches', handle: { title: t.pageTitles.savedSearches }, element: <SavedSearchesPage /> },
      { path: 'saved-searches/:id', element: <OpenSavedSearchPage /> },
      { path: 'deadlines', handle: { title: t.pageTitles.deadlines }, element: <DeadlinesPage /> },
      { path: 'invitations/accept', handle: { title: t.pageTitles.invitationsAccept }, element: <AcceptInvitationPage /> },
      { path: 'styleguide', handle: { title: t.pageTitles.styleguide }, element: <StyleguidePage /> },
      { path: '*', handle: { title: t.pageTitles.notFound }, element: <NotFoundPage /> },
    ],
  },
]
