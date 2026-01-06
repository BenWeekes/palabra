import {DualStreamMode, ToggleState} from '../Contexts/PropsContext';
import {
  ActionType,
  ContentStateInterface,
  UidType,
} from '../Contexts/RtcContext';

export default function UserJoined(
  state: ContentStateInterface,
  action: ActionType<'UserJoined'>,
  dualStreamMode: DualStreamMode,
  localUid: UidType,
) {
  const newUid = action.value[0];

  // TRANSLATION UID FILTER: Skip adding translation/bot UIDs (3000-5999) to activeUids
  // These are Palabra (3000-3999), Anam (4000-4999), and Bot (5000+) streams
  const uidNum = typeof newUid === 'string' ? parseInt(newUid, 10) : newUid;
  const isTranslationUID = uidNum >= 3000 && uidNum < 6000;

  if (isTranslationUID) {
    console.log('[UserJoined] 🚫 Skipping translation/bot UID', newUid, '- not adding to activeUids (no tile)');
    // Return current state unchanged - don't add translation UIDs to the UI
    return state;
  }

  console.log('[UserJoined] ✅ Adding normal UID', newUid, 'to activeUids (will create tile)');

  let stateUpdate = {};
  //default type will be rtc
  let typeData = {
    type: '',
  };
  if (
    state.defaultContent[newUid as unknown as number] &&
    'type' in state.defaultContent[newUid as unknown as number]
  ) {
    typeData.type = state.defaultContent[newUid as unknown as number].type;
  }

  const isExitingUser =
    state?.activeUids?.indexOf(newUid as unknown as number) !== -1;

  let defaultContent: ContentStateInterface['defaultContent'] = {
    ...state.defaultContent,
    [newUid as unknown as number]: {
      ...state.defaultContent[newUid as unknown as number],
      uid: newUid,
      audio: isExitingUser
        ? state?.defaultContent[newUid as unknown as number]?.audio || 0
        : ToggleState.disabled,
      video: isExitingUser
        ? state?.defaultContent[newUid as unknown as number]?.video || 0
        : ToggleState.disabled,
      streamType: dualStreamMode === DualStreamMode.HIGH ? 'high' : 'low', // Low if DualStreamMode is LOW or DYNAMIC by default,
      ...typeData,
    },
  };
  let activeUids = state.activeUids.filter((i) => i === newUid).length
    ? [...state.activeUids]
    : [...state.activeUids, newUid];
  const [maxUid] = activeUids;
  if (activeUids.length === 2 && maxUid === localUid) {
    //Only one remote and local is maximized
    //Change stream type to high if dualStreaMode is DYNAMIC
    if (dualStreamMode === DualStreamMode.DYNAMIC) {
      defaultContent[newUid as unknown as number].streamType = 'high';
    }
    //Swap render positions
    stateUpdate = {
      defaultContent: defaultContent,
      activeUids: activeUids.reverse(),
      lastJoinedUid: newUid,
    };
  } else {
    //More than one remote
    stateUpdate = {
      defaultContent: defaultContent,
      activeUids: activeUids,
      lastJoinedUid: newUid,
    };
  }

  console.log('new user joined!\n', state, stateUpdate, {
    dualStreamMode,
  });
  return stateUpdate;
}
