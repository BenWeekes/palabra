import React, {useContext} from 'react';
import StorageContext from '../../components/StorageContext';
import {useRoomInfo} from '../../components/room-info/useRoomInfo';
import {LanguageTranslationConfig} from './useCaption';
import {PropsContext, useLocalUid} from '../../../agora-rn-uikit';
import {logger, LogSource} from '../../logger/AppBuilderLogger';
import getUniqueID from '../../utils/getUniqueID';

export interface PalabraAPIResponse {
  success: boolean;
  data?: any;
  error?: {
    message: string;
    code?: number;
  };
}

interface IusePalabraAPI {
  start: (
    botUid: number,
    translationConfig: LanguageTranslationConfig,
  ) => Promise<PalabraAPIResponse>;
  stop: (botUid: number) => Promise<PalabraAPIResponse>;
}

const usePalabraAPI = (): IusePalabraAPI => {
  const {store} = React.useContext(StorageContext);
  const {
    data: {roomId},
  } = useRoomInfo();
  const {rtcProps} = useContext(PropsContext);
  const localUid = useLocalUid();

  const start = async (
    botUid: number,
    translationConfig: LanguageTranslationConfig,
  ): Promise<PalabraAPIResponse> => {
    const requestId = getUniqueID();
    const startReqTs = Date.now();

    try {
      // Map frontend config to Palabra API format
      const sourceLanguage = translationConfig.source?.[0] || 'en-US';
      const targetLanguages = translationConfig.targets || [];

      const requestBody = {
        channel: roomId?.host || roomId?.attendee || '',
        sourceUid: `${localUid}`,
        sourceLanguage: sourceLanguage,
        targetLanguages: targetLanguages,
      };

      const backendUrl = $config.PALABRA_BACKEND_ENDPOINT || $config.BACKEND_ENDPOINT;
      const response = await fetch(`${backendUrl}/v1/palabra/start`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Request-Id': requestId,
          'X-Session-Id': logger.getSessionId(),
        },
        body: JSON.stringify(requestBody),
      });

      const res = await response.json();
      const endReqTs = Date.now();
      const latency = endReqTs - startReqTs;

      logger.log(
        LogSource.NetworkRest,
        'palabra',
        'Palabra API Success - Started translation',
        {
          responseData: res,
          requestId,
          startReqTs,
          endReqTs,
          latency,
        },
      );

      if (!res.success) {
        return {
          success: false,
          error: {
            message: res.error || 'Failed to start translation',
          },
          data: res,
        };
      }

      return {
        success: true,
        data: res,
      };
    } catch (error) {
      const endReqTs = Date.now();
      const latency = endReqTs - startReqTs;
      logger.error(
        LogSource.NetworkRest,
        'palabra',
        'Palabra API Failure - Start translation failed',
        error,
        {
          requestId,
          startReqTs,
          endReqTs,
          latency,
        },
      );

      return {
        success: false,
        error: {
          message: error?.message || 'Unknown error occurred',
        },
      };
    }
  };

  const stop = async (botUid: number): Promise<PalabraAPIResponse> => {
    const requestId = getUniqueID();
    const startReqTs = Date.now();

    try {
      const backendUrl = $config.PALABRA_BACKEND_ENDPOINT || $config.BACKEND_ENDPOINT;
      const response = await fetch(`${backendUrl}/v1/palabra/stop`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-Request-Id': requestId,
          'X-Session-Id': logger.getSessionId(),
        },
        body: JSON.stringify({}),
      });

      const res = await response.json();
      const endReqTs = Date.now();
      const latency = endReqTs - startReqTs;

      logger.log(
        LogSource.NetworkRest,
        'palabra',
        'Palabra API Success - Stopped translation',
        {
          responseData: res,
          requestId,
          startReqTs,
          endReqTs,
          latency,
        },
      );

      return {
        success: true,
        data: res,
      };
    } catch (error) {
      const endReqTs = Date.now();
      const latency = endReqTs - startReqTs;
      logger.error(
        LogSource.NetworkRest,
        'palabra',
        'Palabra API Failure - Stop translation failed',
        error,
        {
          requestId,
          startReqTs,
          endReqTs,
          latency,
        },
      );

      return {
        success: false,
        error: {
          message: error?.message || 'Unknown error occurred',
        },
      };
    }
  };

  return {
    start,
    stop,
  };
};

export default usePalabraAPI;
