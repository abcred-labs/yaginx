import { FontAwesomeIcon } from '@feinian/fontawesome-free-react/compact';
import type { IconDefinition } from '@feinian/fontawesome-free-react/compact';
import arrowLeft from '@feinian/fontawesome-free-react/icons/solid/arrow-left';
import arrowRight from '@feinian/fontawesome-free-react/icons/solid/arrow-right';
import ban from '@feinian/fontawesome-free-react/icons/solid/ban';
import check from '@feinian/fontawesome-free-react/icons/solid/check';
import chevronDown from '@feinian/fontawesome-free-react/icons/solid/chevron-down';
import chevronUp from '@feinian/fontawesome-free-react/icons/solid/chevron-up';
import circleCheck from '@feinian/fontawesome-free-react/icons/solid/circle-check';
import circleExclamation from '@feinian/fontawesome-free-react/icons/solid/circle-exclamation';
import circleInfo from '@feinian/fontawesome-free-react/icons/solid/circle-info';
import circleMinus from '@feinian/fontawesome-free-react/icons/solid/circle-minus';
import circleXmark from '@feinian/fontawesome-free-react/icons/solid/circle-xmark';
import clipboardList from '@feinian/fontawesome-free-react/icons/solid/clipboard-list';
import copy from '@feinian/fontawesome-free-react/icons/solid/copy';
import database from '@feinian/fontawesome-free-react/icons/solid/database';
import download from '@feinian/fontawesome-free-react/icons/solid/download';
import ellipsis from '@feinian/fontawesome-free-react/icons/solid/ellipsis';
import envelope from '@feinian/fontawesome-free-react/icons/solid/envelope';
import floppyDisk from '@feinian/fontawesome-free-react/icons/solid/floppy-disk';
import gear from '@feinian/fontawesome-free-react/icons/solid/gear';
import globe from '@feinian/fontawesome-free-react/icons/solid/globe';
import house from '@feinian/fontawesome-free-react/icons/solid/house';
import chartLine from '@feinian/fontawesome-free-react/icons/solid/chart-line';
import lock from '@feinian/fontawesome-free-react/icons/solid/lock';
import magnifyingGlass from '@feinian/fontawesome-free-react/icons/solid/magnifying-glass';
import moon from '@feinian/fontawesome-free-react/icons/solid/moon';
import plug from '@feinian/fontawesome-free-react/icons/solid/plug';
import plus from '@feinian/fontawesome-free-react/icons/solid/plus';
import rightFromBracket from '@feinian/fontawesome-free-react/icons/solid/right-from-bracket';
import rotate from '@feinian/fontawesome-free-react/icons/solid/rotate';
import satelliteDish from '@feinian/fontawesome-free-react/icons/solid/satellite-dish';
import shieldHalved from '@feinian/fontawesome-free-react/icons/solid/shield-halved';
import sitemap from '@feinian/fontawesome-free-react/icons/solid/sitemap';
import spinner from '@feinian/fontawesome-free-react/icons/solid/spinner';
import stopwatch from '@feinian/fontawesome-free-react/icons/solid/stopwatch';
import sun from '@feinian/fontawesome-free-react/icons/solid/sun';
import trashCan from '@feinian/fontawesome-free-react/icons/solid/trash-can';
import triangleExclamation from '@feinian/fontawesome-free-react/icons/solid/triangle-exclamation';
import upload from '@feinian/fontawesome-free-react/icons/solid/upload';
import user from '@feinian/fontawesome-free-react/icons/solid/user';
import xmark from '@feinian/fontawesome-free-react/icons/solid/xmark';
import React from 'react';

/**
 * 全站图标出口：Font Awesome Free（CC BY 4.0）。组件名沿用此前的命名，页面只依赖这里，
 * 以后换图标库或升级到 Pro 只改这个文件。
 */
export type IconProps = Omit<React.SVGAttributes<SVGElement>, 'ref'> & { title?: string };
export type Icon = React.FC<IconProps>;

const make = (definition: IconDefinition, displayName: string): Icon => {
  const Component: Icon = ({ className, ...props }) => (
    <FontAwesomeIcon icon={definition} className={className} aria-hidden={props.title ? undefined : 'true'} {...props} />
  );
  Component.displayName = displayName;
  return Component;
};

export const ArrowLeft = make(arrowLeft, 'ArrowLeft');
export const ArrowRight = make(arrowRight, 'ArrowRight');
export const Ban = make(ban, 'Ban');
export const Check = make(check, 'Check');
export const ChevronDown = make(chevronDown, 'ChevronDown');
export const ChevronUp = make(chevronUp, 'ChevronUp');
export const CircleAlert = make(circleExclamation, 'CircleAlert');
export const CircleCheck = make(circleCheck, 'CircleCheck');
export const CircleMinus = make(circleMinus, 'CircleMinus');
export const CircleX = make(circleXmark, 'CircleX');
export const ClipboardList = make(clipboardList, 'ClipboardList');
export const Copy = make(copy, 'Copy');
export const Database = make(database, 'Database');
export const Download = make(download, 'Download');
export const Globe = make(globe, 'Globe');
export const Home = make(house, 'Home');
export const Info = make(circleInfo, 'Info');
export const LineChart = make(chartLine, 'LineChart');
export const Lock = make(lock, 'Lock');
export const LogOut = make(rightFromBracket, 'LogOut');
export const Mail = make(envelope, 'Mail');
export const Moon = make(moon, 'Moon');
export const MoreHorizontal = make(ellipsis, 'MoreHorizontal');
export const Network = make(sitemap, 'Network');
export const Plug = make(plug, 'Plug');
export const Plus = make(plus, 'Plus');
export const Radar = make(satelliteDish, 'Radar');
export const RefreshCw = make(rotate, 'RefreshCw');
export const Save = make(floppyDisk, 'Save');
export const Search = make(magnifyingGlass, 'Search');
export const Settings = make(gear, 'Settings');
export const ShieldCheck = make(shieldHalved, 'ShieldCheck');
export const Spinner = make(spinner, 'Spinner');
export const Sun = make(sun, 'Sun');
export const Timer = make(stopwatch, 'Timer');
export const Trash2 = make(trashCan, 'Trash2');
export const TriangleAlert = make(triangleExclamation, 'TriangleAlert');
export const Upload = make(upload, 'Upload');
export const User = make(user, 'User');
export const UserRound = make(user, 'UserRound');
export const X = make(xmark, 'X');
